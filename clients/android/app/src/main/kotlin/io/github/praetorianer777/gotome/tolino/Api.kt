package io.github.praetorianer777.gotome.tolino

import java.io.File
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder
import java.util.Base64

class ApiException(val status: Int, message: String) : IOException(message)

/** GOtome's HTTP API (api/openapi.json), signed in as a device: the user name and the device's token. */
class Api(private val prefs: Prefs) {
    /**
     * Checks the token with GOtome and keeps it; returns the name the device has
     * there. Nothing is kept when GOtome refuses it.
     */
    fun signIn(server: String, username: String, token: String): String {
        val base = server.trim().trimEnd('/')
        val conn = open("GET", base, "/device", basic(username, token))
        val device = Json.thisDevice(read(conn, signOutOn401 = false))
        prefs.server = base
        prefs.username = device.username
        prefs.token = token
        return device.name
    }

    fun get(path: String): String = read(open("GET", path))

    fun send(method: String, path: String, json: String? = null): String {
        val conn = open(method, path)
        if (json != null) write(conn, json)
        return read(conn)
    }

    fun delete(path: String): String = read(open("DELETE", path))

    /** Streams a file to [target] through a sibling that the reader app does not import. */
    fun download(path: String, target: File) {
        val conn = open("GET", path)
        conn.readTimeout = 120_000
        if (conn.responseCode != 200) read(conn)
        val part = File(target.parentFile, ".${target.name}.part")
        conn.inputStream.use { input -> part.outputStream().use { input.copyTo(it) } }
        if (!part.renameTo(target)) {
            part.delete()
            throw IOException("cannot write ${target.path}")
        }
    }

    fun bytes(path: String): ByteArray? {
        val conn = open("GET", path)
        return if (conn.responseCode == 200) conn.inputStream.use { it.readBytes() } else null
    }

    private fun open(method: String, path: String): HttpURLConnection {
        val token = prefs.token ?: throw ApiException(401, "Not signed in")
        return open(method, prefs.server, path, basic(prefs.username, token))
    }

    private fun open(method: String, server: String, path: String, authorization: String): HttpURLConnection {
        if (server.isEmpty()) throw ApiException(0, "No GOtome server set")
        val conn = URL("$server/api/v1$path").openConnection() as HttpURLConnection
        conn.requestMethod = method
        conn.connectTimeout = 15_000
        conn.readTimeout = 60_000
        // A redirect would carry the token to wherever it points.
        conn.instanceFollowRedirects = false
        conn.setRequestProperty("Accept", "application/json")
        conn.setRequestProperty("User-Agent", "GOtome-Tolino/${BuildConfig.VERSION_NAME}")
        conn.setRequestProperty("Authorization", authorization)
        return conn
    }

    private fun write(conn: HttpURLConnection, json: String) {
        conn.doOutput = true
        conn.setRequestProperty("Content-Type", "application/json")
        conn.outputStream.use { it.write(json.toByteArray()) }
    }

    /** A token revoked in GOtome signs the Tolino out, so that the setup asks for a new one. */
    private fun read(conn: HttpURLConnection, signOutOn401: Boolean = true): String {
        val status = conn.responseCode
        val stream = if (status < 400) conn.inputStream else conn.errorStream
        val body = stream?.bufferedReader()?.use { it.readText() }.orEmpty()
        if (status == 401 && signOutOn401) prefs.token = null
        if (status >= 300) {
            throw ApiException(status, Json.errorMessage(body) ?: "GOtome answered $status")
        }
        return body
    }

    companion object {
        fun basic(username: String, token: String): String =
            "Basic " + Base64.getEncoder().encodeToString("$username:$token".toByteArray(Charsets.UTF_8))

        fun query(vararg pairs: Pair<String, String?>): String = pairs
            .filter { it.second != null }
            .joinToString("&", prefix = "?") { "${it.first}=${URLEncoder.encode(it.second, "UTF-8")}" }
    }
}
