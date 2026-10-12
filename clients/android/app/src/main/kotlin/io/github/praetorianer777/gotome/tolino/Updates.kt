package io.github.praetorianer777.gotome.tolino

import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageInfo
import android.content.pm.PackageInstaller
import android.content.pm.PackageManager
import android.util.Log
import org.json.JSONObject
import java.io.File
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL

data class Release(val version: String, val apk: String)

/**
 * New versions of the app from GOtome's GitHub releases: looked for with a sync,
 * at most once a day, and installed by Android's installer, which asks once.
 * Android only takes an update signed with the installed app's key; the app
 * checks that itself first, so a download signed otherwise never reaches it.
 */
class Updates(private val context: Context) {
    private val prefs = Prefs(context)

    /** Looks for a newer release unless it looked in the last day. */
    fun check(now: Long = System.currentTimeMillis()) {
        if (now - prefs.updateCheckedAt < DAY) return
        val release = latest(get(BuildConfig.RELEASES))
        prefs.updateCheckedAt = now
        prefs.update = release?.takeIf { newer(it.version, BuildConfig.VERSION_NAME) }
    }

    /** Downloads the release, checks it, and hands it to the installer. */
    fun install(release: Release) {
        val apk = File(context.cacheDir, "update.apk")
        download(release.apk, apk)
        val pm = context.packageManager
        val candidate = pm.getPackageArchiveInfo(apk.path, SIGNATURES)
        val installed = pm.getPackageInfo(context.packageName, SIGNATURES)
        val refusal = refusal(
            context.packageName, installed.code(), signers(installed),
            candidate?.packageName, candidate?.code() ?: 0, signers(candidate),
        )
        if (refusal != null) {
            apk.delete()
            throw IOException(context.getString(refusal))
        }
        val installer = pm.packageInstaller
        val params = PackageInstaller.SessionParams(PackageInstaller.SessionParams.MODE_FULL_INSTALL)
            .apply { setAppPackageName(context.packageName) }
        val id = installer.createSession(params)
        installer.openSession(id).use { session ->
            session.openWrite("update.apk", 0, apk.length()).use { out ->
                apk.inputStream().use { it.copyTo(out) }
                session.fsync(out)
            }
            val done = PendingIntent.getBroadcast(
                context, id, Intent(context, InstallResult::class.java), PendingIntent.FLAG_UPDATE_CURRENT,
            )
            session.commit(done.intentSender)
        }
        apk.delete()
    }

    private fun get(address: String): String {
        val conn = open(address)
        conn.setRequestProperty("Accept", "application/vnd.github+json")
        val status = conn.responseCode
        if (status != 200) throw IOException("GitHub answered $status")
        return conn.inputStream.bufferedReader().use { it.readText() }
    }

    private fun download(address: String, target: File) {
        val conn = open(address)
        conn.readTimeout = 120_000
        if (conn.responseCode != 200) throw IOException("GitHub answered ${conn.responseCode}")
        if (conn.contentLengthLong > MAX_APK) throw IOException("The update is too large")
        conn.inputStream.use { input ->
            target.outputStream().use { out ->
                val buffer = ByteArray(64 * 1024)
                var total = 0L
                while (true) {
                    val n = input.read(buffer)
                    if (n < 0) break
                    total += n
                    if (total > MAX_APK) throw IOException("The update is too large")
                    out.write(buffer, 0, n)
                }
            }
        }
    }

    /** GitHub sends a release's files from another host; HttpURLConnection follows within https only. */
    private fun open(address: String): HttpURLConnection {
        val conn = URL(address).openConnection() as HttpURLConnection
        conn.connectTimeout = 15_000
        conn.readTimeout = 30_000
        conn.setRequestProperty("User-Agent", "GOtome-Tolino/${BuildConfig.VERSION_NAME}")
        return conn
    }

    /** Hears how the installer did: asks the person once, and keeps a failure to show. */
    class InstallResult : BroadcastReceiver() {
        override fun onReceive(context: Context, intent: Intent) {
            when (intent.getIntExtra(PackageInstaller.EXTRA_STATUS, PackageInstaller.STATUS_FAILURE)) {
                PackageInstaller.STATUS_PENDING_USER_ACTION -> {
                    @Suppress("DEPRECATION")
                    val confirm = intent.getParcelableExtra<Intent>(Intent.EXTRA_INTENT) ?: return
                    context.startActivity(confirm.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                }
                PackageInstaller.STATUS_SUCCESS -> Prefs(context).update = null
                else -> {
                    val message = intent.getStringExtra(PackageInstaller.EXTRA_STATUS_MESSAGE).orEmpty()
                    Log.w(TAG, "update not installed: $message")
                    Prefs(context).lastError = context.getString(R.string.update_failed, message)
                }
            }
        }
    }

    companion object {
        private const val TAG = "GOtomeUpdates"
        private const val DAY = 24 * 60 * 60 * 1000L
        private const val MAX_APK = 50L * 1024 * 1024
        private const val ASSET = "gotome-tolino-"

        /** The call deprecated since Android 9 is the one Android 8.1 has. */
        @Suppress("DEPRECATION")
        private const val SIGNATURES = PackageManager.GET_SIGNATURES

        /** The release's APK, as release.yml names it; null when it has none. */
        fun latest(body: String): Release? {
            val o = JSONObject(body)
            if (o.optBoolean("draft") || o.optBoolean("prerelease")) return null
            val version = o.optString("tag_name").removePrefix("v")
            if (!VERSION.matches(version)) return null
            val assets = o.optJSONArray("assets") ?: return null
            for (i in 0 until assets.length()) {
                val a = assets.optJSONObject(i) ?: continue
                val url = a.optString("browser_download_url")
                if (a.optString("name") == "$ASSET$version.apk" && url.startsWith("https://")) return Release(version, url)
            }
            return null
        }

        fun newer(version: String, than: String): Boolean {
            val a = parts(version) ?: return false
            val b = parts(than) ?: return true
            for (i in 0 until 3) if (a[i] != b[i]) return a[i] > b[i]
            return false
        }

        /**
         * Why a downloaded APK is not handed to the installer, as a string resource,
         * or null when it may be: this app, a later version, and signed alike.
         */
        fun refusal(
            packageName: String, installedCode: Int, installedSigners: Set<String>,
            candidatePackage: String?, candidateCode: Int, candidateSigners: Set<String>,
        ): Int? = when {
            candidatePackage != packageName -> R.string.update_not_app
            candidateCode <= installedCode -> R.string.update_not_newer
            installedSigners.isEmpty() || candidateSigners != installedSigners -> R.string.update_other_key
            else -> null
        }

        @Suppress("DEPRECATION")
        private fun signers(info: PackageInfo?): Set<String> =
            info?.signatures.orEmpty().map { it.toCharsString() }.toSet()

        @Suppress("DEPRECATION")
        private fun PackageInfo.code(): Int = versionCode

        private val VERSION = Regex("^\\d+\\.\\d+\\.\\d+$")

        private fun parts(v: String): List<Int>? = v.takeIf { VERSION.matches(it) }?.split(".")?.map { it.toInt() }
    }
}
