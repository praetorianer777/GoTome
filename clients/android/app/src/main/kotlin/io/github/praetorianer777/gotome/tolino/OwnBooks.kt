package io.github.praetorianer777.gotome.tolino

import android.content.Context
import android.util.Log
import java.io.File

/**
 * The books on the Tolino that the app did not put there (#211): DRM-free EPUBs
 * and PDFs copied over USB or from older libraries. At a sync they are hashed,
 * GOtome says which of them it has (and counts those as on the device, so their
 * reading progress is reported like a delivered book's), and the rest can be
 * uploaded.
 */
class OwnBooks(private val context: Context, private val db: Db, private val api: Api) {
    private val own = OwnDb.of(context)

    /**
     * Finds, hashes and matches the files. [onMatched] gets each book GOtome has
     * a file of, to fetch its position there. A book whose file left the Tolino is
     * reported gone.
     */
    fun sync(progress: (String) -> Unit, onMatched: (String) -> Unit) = own.use { own ->
        val before = own.matchedBookIds()
        val files = OwnFiles.list(File(Storage.BOOKS), File(Storage.DELIVERED), File(Storage.LAUNCHER))
        val known = own.files().associateBy { it.path }
        val scanned = OwnFiles.scan(files, known, progress = { done, total ->
            progress(context.getString(R.string.own_checking, done + 1, total))
        })
        own.saveFiles(scanned)

        val hashes = scanned.mapNotNull { it.sha256 }.distinct()
        val matches = hashes.chunked(OwnFiles.BATCH).flatMap { batch ->
            OwnFiles.matches(api.send("POST", "/device/matches", OwnFiles.matchesRequest(batch)))
        }
        own.saveMatches(scanned, matches)

        val now = own.matchedBookIds()
        val delivered = db.onDeviceIds()
        for (gone in before - now - delivered) db.report(gone, false)
        for (id in now) onMatched(id)
    }

    /** Whether GOtome would take an upload from this device now, and if not why. */
    fun uploadState(): UploadState = OwnFiles.uploadState(api.get("/device"))

    /**
     * Uploads one file; GOtome stores it, or points to the book that has it
     * already. The book then counts as on the device like a match does.
     */
    fun upload(path: String) = own.use { own ->
        val file = File(path)
        val sha = own.files().firstOrNull { it.path == path }?.sha256 ?: OwnFiles.sha256(file)
        OwnFiles.uploadedBook(api.upload("/device/uploads", file))
        val match = OwnFiles.matches(api.send("POST", "/device/matches", OwnFiles.matchesRequest(listOf(sha))))
            .firstOrNull { it.sha256 == sha }
        if (match == null) {
            Log.w(TAG, "an uploaded file is not found again by its hash")
            return@use
        }
        own.setMatch(path, match)
    }

    companion object {
        private const val TAG = "GOtomeOwn"
    }
}
