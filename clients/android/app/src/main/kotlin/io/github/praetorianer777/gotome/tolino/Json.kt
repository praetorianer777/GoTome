package io.github.praetorianer777.gotome.tolino

import org.json.JSONArray
import org.json.JSONObject

data class BookSummary(
    val id: String,
    val title: String,
    val authors: List<String>,
    val series: String,
    val seriesIndex: Double?,
    val formats: List<String>,
    val addedAt: String,
)

data class BookFile(val id: String, val format: String, val drm: Boolean, val missing: Boolean, val size: Long)

data class BookDetail(
    val id: String,
    val title: String,
    val authors: List<String>,
    val series: String,
    val seriesIndex: Double?,
    val description: String,
    val files: List<BookFile>,
)

data class ThisDevice(val name: String, val username: String)

/** A request to be added to an account: what the app polls with and the code it shows. */
data class Pairing(val server: String, val id: String, val secret: String, val code: String)

/** How GOtome answered about a request; once accepted, what the app signs in with. */
data class PairingAnswer(val state: String, val username: String, val token: String)

data class EbookProgress(val fraction: Double, val chapter: String, val clientId: String, val updatedAt: String)

/**
 * Reads GOtome's answers (api/openapi.json) into what the app keeps of them. A
 * book or file in a list that cannot be read, or whose ID is not one, is left
 * out rather than failing the list: IDs go into addresses and file names.
 */
object Json {
    private val ID = Regex("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")

    fun isId(s: String): Boolean = ID.matches(s)

    private fun JSONObject.id(): String = getString("id").also {
        if (!isId(it)) throw org.json.JSONException("not an ID: $it")
    }

    fun bookList(body: String): Pair<List<BookSummary>, String?> {
        val root = JSONObject(body)
        val next = root.optString("nextCursor").ifEmpty { null }
        return root.getJSONArray("books").each(::bookSummary) to next
    }

    fun bookSummary(o: JSONObject) = BookSummary(
        id = o.id(),
        title = o.getString("title"),
        authors = o.optJSONArray("authors").strings(),
        series = o.optString("series"),
        seriesIndex = if (o.has("seriesIndex")) o.getDouble("seriesIndex") else null,
        formats = o.optJSONArray("formats").strings(),
        addedAt = o.optString("addedAt"),
    )

    /** Search hits describe a book the way a page of books does. */
    fun searchHits(body: String): List<BookSummary> =
        JSONObject(body).getJSONArray("books").each(::bookSummary)

    fun count(body: String): Int = JSONObject(body).getInt("count")

    fun bookDetail(body: String): BookDetail {
        val o = JSONObject(body)
        return BookDetail(
            id = o.id(),
            title = o.getString("title"),
            authors = o.optJSONArray("contributors").objects()
                .filter { it.optString("role") == "author" }
                .map { it.getString("name") },
            series = o.optString("series"),
            seriesIndex = if (o.has("seriesIndex")) o.getDouble("seriesIndex") else null,
            description = o.optString("description"),
            files = o.optJSONArray("files").each {
                BookFile(
                    id = it.id(),
                    format = it.getString("format").lowercase(),
                    drm = it.optBoolean("drm"),
                    missing = it.optBoolean("missing"),
                    size = it.optLong("size"),
                )
            },
        )
    }

    fun summary(book: BookDetail) = BookSummary(
        id = book.id,
        title = book.title,
        authors = book.authors,
        series = book.series,
        seriesIndex = book.seriesIndex,
        formats = book.files.map { it.format }.distinct(),
        addedAt = "",
    )

    /** The file to put on the Tolino: one it can open, without DRM, EPUB before PDF. */
    fun readableFile(book: BookDetail): BookFile? = Storage.READABLE.firstNotNullOfOrNull { format ->
        book.files.firstOrNull { it.format == format && !it.drm && !it.missing }
    }

    fun thisDevice(body: String): ThisDevice {
        val o = JSONObject(body)
        return ThisDevice(o.getJSONObject("device").getString("name"), o.getString("username"))
    }

    fun deviceRequest(username: String, deviceName: String): String =
        JSONObject().put("username", username).put("deviceName", deviceName).toString()

    fun pairing(server: String, body: String): Pairing {
        val o = JSONObject(body)
        return Pairing(server, o.getString("id").also { require(isId(it)) }, o.getString("secret"), o.getString("code"))
    }

    fun secret(secret: String): String = JSONObject().put("secret", secret).toString()

    fun pairingAnswer(body: String): PairingAnswer {
        val o = JSONObject(body)
        return PairingAnswer(o.getString("state"), o.optString("username"), o.optString("token"))
    }

    fun ebookProgress(body: String): EbookProgress? {
        val ebook = JSONObject(body).optJSONObject("ebook") ?: return null
        return EbookProgress(
            fraction = ebook.getDouble("fraction"),
            chapter = ebook.optString("chapter"),
            clientId = ebook.optString("clientId"),
            updatedAt = ebook.optString("updatedAt"),
        )
    }

    /** The web reader opens a place given as "fraction:0.42" at that share of the book. */
    fun progressUpdate(fileId: String, fraction: Double, clientId: String, force: Boolean = false): String = JSONObject()
        .put("fileId", fileId)
        .put("locator", "fraction:$fraction")
        .put("fraction", fraction)
        .put("clientId", clientId)
        .apply { if (force) put("force", true) }
        .toString()

    data class Saved(val saved: Boolean, val clientId: String?)

    /** Whether GOtome kept the position, and whose position it kept otherwise. */
    fun progressSaved(body: String): Saved {
        val o = JSONObject(body)
        return Saved(o.getBoolean("saved"), o.optJSONObject("progress")?.optString("clientId"))
    }

    fun errorMessage(body: String): String? =
        runCatching { JSONObject(body).getJSONObject("error").getString("message") }.getOrNull()

    private fun JSONArray?.objects(): List<JSONObject> =
        if (this == null) emptyList() else (0 until length()).mapNotNull { optJSONObject(it) }

    private fun <T> JSONArray?.each(read: (JSONObject) -> T): List<T> =
        objects().mapNotNull { runCatching { read(it) }.getOrNull() }

    private fun JSONArray?.strings(): List<String> =
        if (this == null) emptyList() else (0 until length()).map { getString(it) }
}
