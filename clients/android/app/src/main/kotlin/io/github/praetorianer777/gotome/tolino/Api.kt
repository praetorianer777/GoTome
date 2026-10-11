package io.github.praetorianer777.gotome.tolino

import java.io.File
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.net.URLEncoder

class ApiException(val status: Int, message: String) : IOException(message)

/** GOtome's HTTP API (api/openapi.json), signed in with the session cookie. */
class Api(private val prefs: Prefs) {
    /** Signs in and keeps the session; the password itself is never stored. */
    fun login(server: String, username: String, password: String) {
        val conn = open("POST", server.trim().trimEnd('/'), "/auth/login", withCookie = false)
        write(conn, Json.login(username, password))
        val body = read(conn)
        val session = conn.headerFields["Set-Cookie"].orEmpty()
            .map { it.substringBefore(';') }
            .firstOrNull { it.startsWith("gotome_session=") }
            ?: throw ApiException(conn.responseCode, "GOtome answered without a session")
        prefs.server = server
        prefs.username = username
        prefs.cookie = session
        prefs.collectionId = null
        check(body.isNotEmpty())
    }

    fun get(path: String): String = read(open("GET", prefs.server, path))

    fun send(method: String, path: String, json: String): String {
        val conn = open(method, prefs.server, path)
        write(conn, json)
        return read(conn)
    }

    /** Streams a file to [target] through a sibling that the reader app does not import. */
    fun download(path: String, target: File) {
        val conn = open("GET", prefs.server, path)
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
        val conn = open("GET", prefs.server, path)
        return if (conn.responseCode == 200) conn.inputStream.use { it.readBytes() } else null
    }

    private fun open(method: String, server: String, path: String, withCookie: Boolean = true): HttpURLConnection {
        if (server.isEmpty()) throw ApiException(0, "No GOtome server set")
        val conn = URL("$server/api/v1$path").openConnection() as HttpURLConnection
        conn.requestMethod = method
        conn.connectTimeout = 15_000
        conn.readTimeout = 60_000
        conn.instanceFollowRedirects = false
        conn.setRequestProperty("Accept", "application/json")
        conn.setRequestProperty("User-Agent", "GOtome-Tolino/${BuildConfig.VERSION_NAME}")
        if (withCookie) prefs.cookie?.let { conn.setRequestProperty("Cookie", it) }
        return conn
    }

    private fun write(conn: HttpURLConnection, json: String) {
        conn.doOutput = true
        conn.setRequestProperty("Content-Type", "application/json")
        conn.outputStream.use { it.write(json.toByteArray()) }
    }

    private fun read(conn: HttpURLConnection): String {
        val status = conn.responseCode
        val stream = if (status < 400) conn.inputStream else conn.errorStream
        val body = stream?.bufferedReader()?.use { it.readText() }.orEmpty()
        if (status == 401) prefs.cookie = null
        if (status >= 300) {
            throw ApiException(status, Json.errorMessage(body) ?: "GOtome answered $status")
        }
        return body
    }

    companion object {
        fun query(vararg pairs: Pair<String, String?>): String = pairs
            .filter { it.second != null }
            .joinToString("&", prefix = "?") { "${it.first}=${URLEncoder.encode(it.second, "UTF-8")}" }
    }
}
