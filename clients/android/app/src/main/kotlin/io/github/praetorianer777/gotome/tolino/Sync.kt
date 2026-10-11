package io.github.praetorianer777.gotome.tolino

import android.content.Context
import android.util.Log
import java.io.File

/**
 * Everything that talks to GOtome in the background: positions out and the
 * device's collection in. One sync runs at a time, whoever starts it.
 */
class Sync(private val context: Context) {
    private val prefs = Prefs(context)
    private val db = Db(context)
    private val api = Api(prefs)

    /** Syncs, telling [progress] what it is doing in words for the person. */
    fun run(progress: (String) -> Unit = {}) = synchronized(LOCK) {
        if (!prefs.signedIn) return
        // Deleted in the Tolino library, it would leave the store without its way in.
        runCatching { LauncherBook.ensure(context) }
        try {
            progress(context.getString(R.string.sending_progress))
            sendProgress()
            // Before delivering, or a book just taken off would come straight back.
            uncollect()
            deliverCollection(progress)
            fetchPositions()
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
        val file = Json.readableFile(book) ?: throw ApiException(0, context.getString(R.string.no_readable_file))
        val dir = File(Storage.DELIVERED).apply { mkdirs() }
        val target = File(dir, Storage.fileName(book.id, book.title, book.authors, file.format))
        api.download("/files/${file.id}/download", target)
        return Delivered(book.id, file.id, target.path).also {
            db.addDelivered(it, Json.summary(book))
            runCatching { addToCollection(bookId) }
        }
    }

    /**
     * Takes a book off the Tolino: its file goes, which takes it out of the Tolino
     * library, and it leaves the device's collection, now or with the next sync.
     */
    fun remove(bookId: String) = synchronized(LOCK) {
        db.delivered(bookId)?.let { File(it.path).delete() }
        db.forget(bookId)
        if (prefs.signedIn && Network.online(context)) runCatching { uncollect() }
    }

    fun ebookProgress(bookId: String): EbookProgress? = Json.ebookProgress(api.get("/books/$bookId/progress"))

    private fun sendProgress() {
        for (u in db.unsent()) {
            try {
                val path = "/books/${u.bookId}/progress/ebook"
                val answer = Json.progressSaved(api.send("PUT", path, Json.progressUpdate(u.fileId, u.fraction, prefs.clientId)))
                // GOtome keeps a further position from elsewhere; one this Tolino sent
                // itself is where it was, and where it is now replaces it.
                if (!answer.saved && answer.clientId == prefs.clientId) {
                    api.send("PUT", path, Json.progressUpdate(u.fileId, u.fraction, prefs.clientId, force = true))
                }
            } catch (e: ApiException) {
                // A book deleted in GOtome takes its position with it.
                if (e.status != 404) throw e
            }
            db.markSent(u)
        }
    }

    private fun deliverCollection(progress: (String) -> Unit) {
        progress(context.getString(R.string.checking_collection, prefs.collectionName))
        val have = db.deliveredIds()
        val missing = Json.collectionBooks(api.get("/collections/${collectionId()}"))
            .filter { it.id !in have && Storage.READABLE.any { format -> format in it.formats } }
        for ((i, book) in missing.withIndex()) {
            progress(context.getString(R.string.loading_book, i + 1, missing.size, book.title))
            try {
                deliver(book.id)
            } catch (e: ApiException) {
                Log.w(TAG, "cannot deliver ${book.id}: ${e.message}")
            }
        }
    }

    /** GOtome's position in one book on the Tolino, kept for offering a jump when it is opened. */
    fun fetchPosition(bookId: String) {
        val p = try {
            ebookProgress(bookId)
        } catch (e: ApiException) {
            if (e.status != 404) throw e
            null
        }
        db.saveRemote(bookId, p?.let { Remote(it.fraction, it.clientId, it.updatedAt) })
    }

    private fun fetchPositions() {
        for (id in db.onDeviceIds()) fetchPosition(id)
    }

    private fun uncollect() {
        for (id in db.toUncollect()) {
            try {
                api.delete("/collections/${collectionId()}/books/$id")
            } catch (e: ApiException) {
                if (e.status != 404) throw e
            }
            db.uncollected(id)
        }
    }

    private fun addToCollection(bookId: String) {
        api.send("POST", "/collections/${collectionId()}/books", """{"books":["$bookId"]}""")
    }

    /** The person's collection of the chosen name, made when there is none. */
    private fun collectionId(): String {
        prefs.collectionId?.let { return it }
        val name = prefs.collectionName
        val found = Json.collections(api.get("/collections")).firstOrNull { it.mine && it.name.equals(name, ignoreCase = true) }
        val id = found?.id ?: Json.collectionId(api.send("POST", "/collections", Json.newCollection(name)))
        prefs.collectionId = id
        return id
    }

    companion object {
        private const val TAG = "GOtomeSync"
        private val LOCK = Any()
    }
}
