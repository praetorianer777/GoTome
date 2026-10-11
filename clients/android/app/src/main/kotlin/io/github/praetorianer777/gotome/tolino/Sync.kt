package io.github.praetorianer777.gotome.tolino

import android.content.Context
import android.util.Log
import java.io.File

/**
 * Everything that talks to GOtome: positions out, the device's collection in,
 * and the catalogue refreshed. One sync runs at a time, whoever starts it.
 */
class Sync(context: Context) {
    private val prefs = Prefs(context)
    private val db = Db(context)
    private val api = Api(prefs)

    fun run(refreshCatalogue: Boolean = false) = synchronized(LOCK) {
        if (!prefs.signedIn) return
        try {
            sendProgress()
            deliverCollection()
            if (refreshCatalogue || System.currentTimeMillis() - prefs.catalogueAt > CATALOGUE_AGE) {
                refreshCatalogue()
            }
            prefs.lastSync = System.currentTimeMillis()
            prefs.lastError = null
        } catch (e: Exception) {
            Log.w(TAG, "sync failed", e)
            prefs.lastError = e.message ?: e.javaClass.simpleName
        }
    }

    /**
     * Puts one book on the Tolino and into the device's collection, so that the web
     * shows what is on the device. The reader app imports it within seconds.
     */
    fun deliver(bookId: String): Delivered = synchronized(LOCK) {
        db.delivered(bookId)?.takeIf { File(it.path).exists() }?.let { return it }
        val book = Json.bookDetail(api.get("/books/$bookId"))
        val file = Json.readableFile(book) ?: throw ApiException(0, "No EPUB or PDF without DRM")
        val dir = File(Storage.DELIVERED).apply { mkdirs() }
        val target = File(dir, Storage.fileName(book.id, book.title, book.authors, file.format))
        api.download("/files/${file.id}/download", target)
        return Delivered(book.id, file.id, target.path).also {
            db.addDelivered(it)
            runCatching { addToCollection(bookId) }
        }
    }

    fun ebookProgress(bookId: String): EbookProgress? = Json.ebookProgress(api.get("/books/$bookId/progress"))

    private fun sendProgress() {
        for (u in db.unsent()) {
            try {
                api.send("PUT", "/books/${u.bookId}/progress/ebook", Json.progressUpdate(u.fileId, u.fraction, prefs.clientId))
            } catch (e: ApiException) {
                // A book deleted in GOtome takes its position with it.
                if (e.status != 404) throw e
            }
            db.markSent(u)
        }
    }

    private fun deliverCollection() {
        val id = collectionId()
        val have = db.deliveredIds()
        for (book in Json.collectionBooks(api.get("/collections/$id"))) {
            if (book.id in have || Storage.READABLE.none { it in book.formats }) continue
            try {
                deliver(book.id)
            } catch (e: ApiException) {
                Log.w(TAG, "cannot deliver ${book.id}: ${e.message}")
            }
        }
    }

    private fun refreshCatalogue() {
        val filter = """{"field":"format","op":"in","values":["epub","pdf"]}"""
        val books = mutableListOf<BookSummary>()
        var cursor: String? = null
        do {
            val (page, next) = Json.bookList(
                api.get("/books" + Api.query("sort" to "added", "order" to "desc", "limit" to "200", "filter" to filter, "cursor" to cursor)),
            )
            books += page
            cursor = next
        } while (cursor != null)
        db.replaceCatalogue(books)
        prefs.catalogueAt = System.currentTimeMillis()
    }

    private fun addToCollection(bookId: String) {
        api.send("POST", "/collections/${collectionId()}/books", """{"books":["$bookId"]}""")
    }

    /** The person's collection of the chosen name, made when there is none. */
    private fun collectionId(): String {
        prefs.collectionId?.let { return it }
        val name = prefs.collectionName
        val found = Json.collections(api.get("/collections")).firstOrNull { it.mine && it.name.equals(name, ignoreCase = true) }
        val id = found?.id ?: org.json.JSONObject(api.send("POST", "/collections", Json.newCollection(name))).getString("id")
        prefs.collectionId = id
        return id
    }

    companion object {
        private const val TAG = "GOtomeSync"
        private const val CATALOGUE_AGE = 12 * 60 * 60 * 1000L
        private val LOCK = Any()
    }
}
