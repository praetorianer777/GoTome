package io.github.praetorianer777.gotome.tolino

import android.content.Context
import java.util.UUID

/** What the app remembers between starts: where GOtome is, the device's token, and how syncing went. */
class Prefs(context: Context) {
    private val p = context.getSharedPreferences("gotome", Context.MODE_PRIVATE).apply {
        // Versions before device tokens kept a whole session and the collection used as the queue.
        if (contains("cookie")) edit().remove("cookie").remove("collectionName").remove("collectionId").apply()
    }

    var server: String
        get() = p.getString("server", "")!!
        set(value) = p.edit().putString("server", value.trim().trimEnd('/')).apply()

    /** Lang.ENGLISH or Lang.GERMAN. */
    var language: String
        get() = p.getString("language", Lang.ENGLISH)!!
        set(value) = p.edit().putString("language", value).apply()

    var username: String
        get() = p.getString("username", "")!!
        set(value) = p.edit().putString("username", value).apply()

    /**
     * The device token made for this Tolino on the profile in GOtome; null when
     * signed out. It reaches only what a device needs, and revoking it there
     * signs the Tolino out.
     */
    var token: String?
        get() = p.getString("token", null)
        set(value) = p.edit().putString("token", value).apply()

    /** Names this device in GOtome's progress, so the web can tell its positions apart. */
    val clientId: String
        get() = p.getString("clientId", null) ?: "tolino-${UUID.randomUUID().toString().take(8)}".also {
            p.edit().putString("clientId", it).apply()
        }

    var lastSync: Long
        get() = p.getLong("lastSync", 0)
        set(value) = p.edit().putLong("lastSync", value).apply()

    var lastError: String?
        get() = p.getString("lastError", null)
        set(value) = p.edit().putString("lastError", value).apply()

    var catalogueAt: Long
        get() = p.getLong("catalogueAt", 0)
        set(value) = p.edit().putLong("catalogueAt", value).apply()

    /** When a line of the reader app was last read from the log; 0 for never. */
    var readerSeenAt: Long
        get() = p.getLong("readerSeenAt", 0)
        set(value) = p.edit().putLong("readerSeenAt", value).apply()

    var updateCheckedAt: Long
        get() = p.getLong("updateCheckedAt", 0)
        set(value) = p.edit().putLong("updateCheckedAt", value).apply()

    /** A newer release of the app, once found; null while there is none. */
    var update: Release?
        get() {
            val version = p.getString("updateVersion", null) ?: return null
            return Release(version, p.getString("updateApk", null) ?: return null)
        }
        set(value) = p.edit().putString("updateVersion", value?.version).putString("updateApk", value?.apk).apply()

    val signedIn: Boolean
        get() = server.isNotEmpty() && username.isNotEmpty() && token != null
}
