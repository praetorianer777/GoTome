package io.github.praetorianer777.gotome.tolino

import android.content.Context
import java.util.UUID

/** What the app remembers between starts: where GOtome is, the session, and how syncing went. */
class Prefs(context: Context) {
    private val p = context.getSharedPreferences("gotome", Context.MODE_PRIVATE)

    var server: String
        get() = p.getString("server", "")!!
        set(value) = p.edit().putString("server", value.trim().trimEnd('/')).apply()

    var username: String
        get() = p.getString("username", "")!!
        set(value) = p.edit().putString("username", value).apply()

    /** The session cookie as it goes into the Cookie header; null when signed out. */
    var cookie: String?
        get() = p.getString("cookie", null)
        set(value) = p.edit().putString("cookie", value).apply()

    var collectionName: String
        get() = p.getString("collectionName", "Tolino")!!
        set(value) = p.edit().putString("collectionName", value.trim()).apply()

    var collectionId: String?
        get() = p.getString("collectionId", null)
        set(value) = p.edit().putString("collectionId", value).apply()

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

    val signedIn: Boolean
        get() = server.isNotEmpty() && cookie != null
}
