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

data class CollectionInfo(val id: String, val name: String, val mine: Boolean)

data class EbookProgress(val fraction: Double, val chapter: String, val clientId: String, val updatedAt: String)

/** Reads GOtome's answers (api/openapi.json) into what the app keeps of them. */
object Json {
    fun bookList(body: String): Pair<List<BookSummary>, String?> {
        val root = JSONObject(body)
        val next = root.optString("nextCursor").ifEmpty { null }
        return root.getJSONArray("books").objects().map(::bookSummary) to next
    }

    fun bookSummary(o: JSONObject) = BookSummary(
        id = o.getString("id"),
        title = o.getString("title"),
        authors = o.optJSONArray("authors").strings(),
        series = o.optString("series"),
        seriesIndex = if (o.has("seriesIndex")) o.getDouble("seriesIndex") else null,
        formats = o.optJSONArray("formats").strings(),
        addedAt = o.optString("addedAt"),
    )

    fun bookDetail(body: String): BookDetail {
        val o = JSONObject(body)
        return BookDetail(
            id = o.getString("id"),
            title = o.getString("title"),
            authors = o.optJSONArray("contributors").objects()
                .filter { it.optString("role") == "author" }
                .map { it.getString("name") },
            series = o.optString("series"),
            seriesIndex = if (o.has("seriesIndex")) o.getDouble("seriesIndex") else null,
            description = o.optString("description"),
            files = o.optJSONArray("files").objects().map {
                BookFile(
                    id = it.getString("id"),
                    format = it.getString("format").lowercase(),
                    drm = it.optBoolean("drm"),
                    missing = it.optBoolean("missing"),
                    size = it.optLong("size"),
                )
            },
        )
    }

    /** The file to put on the Tolino: one it can open, without DRM, EPUB before PDF. */
    fun readableFile(book: BookDetail): BookFile? = Storage.READABLE.firstNotNullOfOrNull { format ->
        book.files.firstOrNull { it.format == format && !it.drm && !it.missing }
    }

    fun collections(body: String): List<CollectionInfo> =
        JSONObject(body).getJSONArray("collections").objects().map(::collection)

    fun collection(o: JSONObject) = CollectionInfo(o.getString("id"), o.getString("name"), o.optBoolean("mine"))

    fun collectionBooks(body: String): List<BookSummary> =
        JSONObject(body).getJSONArray("bookList").objects().map(::bookSummary)

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
    fun progressUpdate(fileId: String, fraction: Double, clientId: String): String = JSONObject()
        .put("fileId", fileId)
        .put("locator", "fraction:$fraction")
        .put("fraction", fraction)
        .put("clientId", clientId)
        .toString()

    fun login(username: String, password: String): String =
        JSONObject().put("username", username).put("password", password).toString()

    fun newCollection(name: String): String =
        JSONObject().put("name", name).put("visibility", "private").toString()

    fun errorMessage(body: String): String? =
        runCatching { JSONObject(body).getJSONObject("error").getString("message") }.getOrNull()

    private fun JSONArray?.objects(): List<JSONObject> =
        if (this == null) emptyList() else (0 until length()).map { getJSONObject(it) }

    private fun JSONArray?.strings(): List<String> =
        if (this == null) emptyList() else (0 until length()).map { getString(it) }
}
