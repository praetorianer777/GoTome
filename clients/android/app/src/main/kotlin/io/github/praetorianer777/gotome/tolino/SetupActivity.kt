package io.github.praetorianer777.gotome.tolino

import android.content.Context
import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.RadioGroup
import android.widget.TextView
import java.text.DateFormat
import java.util.Date

/**
 * Where GOtome is and whose account this Tolino is added to: asked for and
 * accepted on the person's profile, or with a device token typed by hand.
 * Also whether the app can see the reader, its language and its updates.
 */
class SetupActivity : Activity() {
    override fun attachBaseContext(base: Context) {
        super.attachBaseContext(Lang.wrap(base))
    }

    private lateinit var prefs: Prefs
    private lateinit var server: EditText
    private lateinit var username: EditText
    private lateinit var token: EditText
    private lateinit var status: TextView
    private val main = Handler(Looper.getMainLooper())
    private var asking = false
    private val poll = Runnable { askAboutPairing() }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        prefs = Prefs(this)
        setContentView(R.layout.activity_setup)
        server = findViewById(R.id.server)
        username = findViewById(R.id.username)
        token = findViewById(R.id.token)
        status = findViewById(R.id.status)
        findViewById<TextView>(R.id.version).text = getString(R.string.version, BuildConfig.VERSION_NAME)
        findViewById<RadioGroup>(R.id.language).apply {
            check(if (prefs.language == Lang.GERMAN) R.id.german else R.id.english)
            setOnCheckedChangeListener { _, id ->
                val chosen = if (id == R.id.german) Lang.GERMAN else Lang.ENGLISH
                if (chosen != prefs.language) {
                    prefs.language = chosen
                    recreate()
                }
            }
        }
        server.setText(prefs.server)
        username.setText(prefs.username)
        findViewById<Button>(R.id.sign_in).setOnClickListener { signIn() }
        findViewById<Button>(R.id.request).setOnClickListener { requestAccess() }
        findViewById<Button>(R.id.use_token).setOnClickListener {
            val group = findViewById<View>(R.id.token_group)
            group.visibility = if (group.visibility == View.VISIBLE) View.GONE else View.VISIBLE
        }
        findViewById<Button>(R.id.update).setOnClickListener { update() }
        findViewById<Button>(R.id.store).setOnClickListener {
            startActivity(Intent(this, StoreActivity::class.java))
            finish()
        }
        val storage = arrayOf(Manifest.permission.READ_EXTERNAL_STORAGE, Manifest.permission.WRITE_EXTERNAL_STORAGE)
        if (storage.any { checkSelfPermission(it) != PackageManager.PERMISSION_GRANTED }) requestPermissions(storage, 1)
    }

    override fun onResume() {
        super.onResume()
        showStatus()
        showPairing()
        if (prefs.pairing != null) main.post(poll)
    }

    override fun onPause() {
        super.onPause()
        main.removeCallbacks(poll)
    }

    /** Asks GOtome for this Tolino to be added; the person accepts it on their profile by its code. */
    private fun requestAccess() {
        val address = server.text.toString().trim()
        val user = username.text.toString().trim()
        status.text = getString(R.string.requesting)
        Thread {
            val result = runCatching {
                if (!Network.online(this)) throw ApiException(0, getString(R.string.offline))
                checkScheme(address)
                Api(prefs).requestDevice(address, user, deviceName())
            }
            runOnUiThread {
                if (isFinishing) return@runOnUiThread
                result.onSuccess {
                    prefs.pairing = it
                    showPairing()
                    main.removeCallbacks(poll)
                    main.postDelayed(poll, POLL)
                }
                showStatus(result.exceptionOrNull()?.message?.let { getString(R.string.request_failed, it) })
            }
        }.start()
    }

    /** Asks GOtome whether the request was decided, every few seconds while the page is open. */
    private fun askAboutPairing() {
        val pairing = prefs.pairing ?: return
        if (asking) return
        asking = true
        Thread {
            val answer = runCatching { Api(prefs).collect(pairing) }
            runOnUiThread {
                asking = false
                if (isFinishing) return@runOnUiThread
                val a = answer.getOrNull()
                when {
                    // Unreachable for now: ask again, as GOtome keeps the request for a while.
                    a == null -> main.postDelayed(poll, POLL)
                    a.state == "pending" -> main.postDelayed(poll, POLL)
                    a.state == "accepted" -> {
                        prefs.pairing = null
                        showPairing()
                        signIn(pairing.server, a.username, a.token)
                    }
                    else -> {
                        prefs.pairing = null
                        showPairing()
                        showStatus(getString(if (a.state == "declined") R.string.pairing_declined else R.string.pairing_expired))
                    }
                }
            }
        }.start()
    }

    private fun showPairing() {
        val pairing = prefs.pairing
        findViewById<View>(R.id.pairing).visibility = if (pairing == null) View.GONE else View.VISIBLE
        findViewById<TextView>(R.id.code).text = pairing?.code.orEmpty()
    }

    private fun signIn() {
        signIn(server.text.toString().trim(), username.text.toString().trim(), token.text.toString().trim())
    }

    private fun signIn(address: String, user: String, key: String) {
        status.text = getString(R.string.signing_in)
        Thread {
            val result = runCatching {
                if (!Network.online(this)) throw ApiException(0, getString(R.string.offline))
                checkScheme(address)
                Api(prefs).signIn(address, user, key)
                LauncherBook.ensure(this)
                Sync(this).run { step -> runOnUiThread { status.text = step } }
            }
            runOnUiThread {
                if (isFinishing) return@runOnUiThread
                token.setText("")
                username.setText(prefs.username)
                result.onSuccess {
                    ReaderWatcher.start(this)
                    SyncJob.schedule(this)
                }
                showStatus(result.exceptionOrNull()?.message?.let { getString(R.string.sign_in_failed, it) })
            }
        }.start()
    }

    /** Looks for a release now, then offers it; with one found, downloads it and hands it to Android's installer, which asks once. */
    private fun update() {
        val button = findViewById<Button>(R.id.update)
        val release = prefs.update ?: return check(button)
        button.isEnabled = false
        status.text = getString(R.string.updating, release.version)
        Thread {
            val result = runCatching {
                if (!Network.online(this)) throw ApiException(0, getString(R.string.offline))
                Updates(this).install(release)
            }
            runOnUiThread {
                if (isFinishing) return@runOnUiThread
                button.isEnabled = true
                result.onFailure { showStatus(getString(R.string.update_failed, it.message ?: "")) }
            }
        }.start()
    }

    private fun check(button: Button) {
        button.isEnabled = false
        status.text = getString(R.string.update_checking)
        Thread {
            val result = runCatching {
                if (!Network.online(this)) throw ApiException(0, getString(R.string.offline))
                Updates(this).check(force = true)
            }
            runOnUiThread {
                if (isFinishing) return@runOnUiThread
                button.isEnabled = true
                showStatus(
                    result.exceptionOrNull()?.let { getString(R.string.update_check_failed, it.message ?: "") }
                        ?: getString(R.string.up_to_date, BuildConfig.VERSION_NAME).takeIf { prefs.update == null },
                )
            }
        }.start()
    }

    private fun showStatus(error: String? = null) {
        findViewById<Button>(R.id.update).text =
            prefs.update?.let { getString(R.string.update_to, it.version) } ?: getString(R.string.update_check)
        val lines = mutableListOf<String>()
        error?.let { lines += it }
        lines += if (prefs.signedIn) getString(R.string.signed_in_as, prefs.username, prefs.server)
        else getString(R.string.signed_out)
        if (prefs.signedIn && prefs.server.startsWith("http://")) lines += getString(R.string.cleartext)
        lines += if (ReaderWatcher.canReadLog(this)) getString(R.string.log_granted)
        else getString(R.string.log_missing, packageName)
        lines += if (JumpService.enabled(this)) getString(R.string.jump_on) else getString(R.string.jump_off, packageName)
        lines += if (prefs.readerSeenAt == 0L) getString(R.string.reader_never)
        else getString(R.string.reader_seen, DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.SHORT).format(Date(prefs.readerSeenAt)))
        prefs.lastError?.let { lines += getString(R.string.sync_failed, it) }
        status.text = lines.joinToString("\n\n")
    }

    private fun checkScheme(address: String) {
        if (!address.startsWith("https://") && !address.startsWith("http://")) {
            throw ApiException(0, getString(R.string.server_scheme))
        }
    }

    /** What the device is called on the profile: the model, which on a Tolino names it. */
    private fun deviceName(): String {
        val model = Build.MODEL.orEmpty().trim()
        return when {
            model.isEmpty() -> "Tolino"
            model.contains("tolino", ignoreCase = true) -> model
            else -> "Tolino $model"
        }
    }

    private companion object {
        const val POLL = 3_000L
    }
}
