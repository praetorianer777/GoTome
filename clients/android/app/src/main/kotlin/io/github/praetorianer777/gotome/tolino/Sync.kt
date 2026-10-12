package io.github.praetorianer777.gotome.tolino

import android.content.Context
import android.util.Log
import java.io.File

/**
 * Everything that talks to GOtome in the background: positions out, the books
 * sent to this device in, and which books are on it back to GOtome. One sync
 * runs at a time, whoever starts it.
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
            deliverQueue(progress)
            report()
            fetchPositions()
            prefs.lastSync = System.currentTimeMillis()
            prefs.lastError = null
        } catch (e: Exception) {
            Log.w(TAG, "sync failed", e)
            prefs.lastError = e.message ?: e.javaClass.simpleName
        }
    }

    /**
     * Puts one book on the Tolino, which the reader app imports within seconds, and
     * tells GOtome, whose page of the book then says it is on this device.
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
            runCatching { report() }
        }
    }

    /**
     * Takes a book off the Tolino: its file goes, which takes it out of the Tolino
     * library, and GOtome is told, now or with the next sync.
     */
    fun remove(bookId: String) = synchronized(LOCK) {
        db.delivered(bookId)?.let { File(it.path).delete() }
        db.forget(bookId)
        if (prefs.signedIn && Network.online(context)) runCatching { report() }
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

    /**
     * Fetches what was sent to this device. A book that is here already, because
     * GOtome was not told after its download, is only reported.
     */
    private fun deliverQueue(progress: (String) -> Unit) {
        progress(context.getString(R.string.checking_queue))
        val (queued, _) = Json.bookList(api.get("/device/books"))
        val missing = queued.filter { book ->
            val here = db.delivered(book.id)?.takeIf { File(it.path).exists() }
            if (here != null) db.report(book.id, true)
            here == null
        }
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

    /** Tells GOtome which books came onto the Tolino and which left it. */
    private fun report() {
        for ((id, present) in db.reports()) {
            try {
                if (present) api.send("PUT", "/device/books/$id") else api.delete("/device/books/$id")
            } catch (e: ApiException) {
                // A book deleted in GOtome, or no longer seen, has nothing to say.
                if (e.status != 404) throw e
            }
            db.reported(id, present)
        }
    }

    companion object {
        private const val TAG = "GOtomeSync"
        private val LOCK = Any()
    }
}
