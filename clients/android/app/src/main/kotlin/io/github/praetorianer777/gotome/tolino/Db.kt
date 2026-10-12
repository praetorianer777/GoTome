package io.github.praetorianer777.gotome.tolino

import android.content.ContentValues
import android.content.Context
import android.database.Cursor
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper
import java.io.File

data class Delivered(val bookId: String, val fileId: String, val path: String)

data class Unsent(val bookId: String, val fileId: String, val fraction: Double, val at: Long)

/**
 * What the app keeps on the device: the books it put on the Tolino, described
 * well enough to list them without network, positions not yet sent to GOtome,
 * and GOtome's positions in those books as of the last sync, so that a jump can
 * be offered offline. The library itself stays on the server.
 */
class Db(context: Context) : SQLiteOpenHelper(context, "gotome.db", null, 5) {
    override fun onCreate(db: SQLiteDatabase) {
        db.execSQL(
            """CREATE TABLE delivered (book_id TEXT PRIMARY KEY, file_id TEXT NOT NULL,
               path TEXT NOT NULL UNIQUE, at INTEGER NOT NULL, removed INTEGER NOT NULL DEFAULT 0,
               title TEXT NOT NULL DEFAULT '', authors TEXT NOT NULL DEFAULT '',
               series TEXT NOT NULL DEFAULT '', series_index REAL, formats TEXT NOT NULL DEFAULT '')""",
        )
        db.execSQL(
            """CREATE TABLE progress (book_id TEXT PRIMARY KEY, file_id TEXT NOT NULL,
               fraction REAL NOT NULL, at INTEGER NOT NULL, sent INTEGER NOT NULL DEFAULT 0)""",
        )
        createRemote(db)
        createReport(db)
    }

    /**
     * Version 1 kept the whole catalogue on the device, version 2 no positions from
     * GOtome, and version 4 a collection as the device's queue instead of GOtome's
     * deliveries: what is on the Tolino is told to GOtome once.
     */
    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) {
        if (oldVersion < 2) {
            db.execSQL("DROP TABLE IF EXISTS books")
            for (column in listOf(
                "title TEXT NOT NULL DEFAULT ''", "authors TEXT NOT NULL DEFAULT ''",
                "series TEXT NOT NULL DEFAULT ''", "series_index REAL", "formats TEXT NOT NULL DEFAULT ''",
            )) db.execSQL("ALTER TABLE delivered ADD COLUMN $column")
        }
        if (oldVersion < 3) createRemote(db)
        if (oldVersion < 5) {
            db.execSQL("DROP TABLE IF EXISTS uncollect")
            createReport(db)
            db.execSQL("INSERT INTO report (book_id, present) SELECT book_id, 1 FROM delivered WHERE removed = 0")
        }
    }

    /**
     * Whether a book is on the Tolino, as GOtome is still to be told: its page in
     * the web says which books the device has.
     */
    private fun createReport(db: SQLiteDatabase) {
        db.execSQL("CREATE TABLE report (book_id TEXT PRIMARY KEY, present INTEGER NOT NULL)")
    }

    fun report(bookId: String, present: Boolean) {
        writableDatabase.insertWithOnConflict("report", null, ContentValues().apply {
            put("book_id", bookId)
            put("present", if (present) 1 else 0)
        }, SQLiteDatabase.CONFLICT_REPLACE)
    }

    fun reports(): List<Pair<String, Boolean>> =
        readableDatabase.rawQuery("SELECT book_id, present FROM report", null).use { c ->
            generateSequence { if (c.moveToNext()) c.getString(0) to (c.getInt(1) == 1) else null }.toList()
        }

    /** Marks a report sent, unless it changed since it was read. */
    fun reported(bookId: String, present: Boolean) {
        writableDatabase.delete("report", "book_id = ? AND present = ?", arrayOf(bookId, if (present) "1" else "0"))
    }

    /** Forgets a book taken off the Tolino in the app; sending it again brings it back. */
    fun forget(bookId: String) {
        writableDatabase.delete("delivered", "book_id = ?", arrayOf(bookId))
        writableDatabase.delete("remote", "book_id = ?", arrayOf(bookId))
        report(bookId, false)
    }

    private fun createRemote(db: SQLiteDatabase) {
        db.execSQL(
            """CREATE TABLE remote (book_id TEXT PRIMARY KEY, fraction REAL NOT NULL,
               client_id TEXT NOT NULL, updated_at TEXT NOT NULL)""",
        )
        db.execSQL("CREATE TABLE asked (offer TEXT PRIMARY KEY)")
    }

    fun saveRemote(bookId: String, remote: Remote?) {
        if (remote == null) {
            writableDatabase.delete("remote", "book_id = ?", arrayOf(bookId))
            return
        }
        writableDatabase.insertWithOnConflict("remote", null, ContentValues().apply {
            put("book_id", bookId)
            put("fraction", remote.fraction)
            put("client_id", remote.clientId)
            put("updated_at", remote.updatedAt)
        }, SQLiteDatabase.CONFLICT_REPLACE)
    }

    fun remote(bookId: String): Remote? =
        readableDatabase.rawQuery("SELECT fraction, client_id, updated_at FROM remote WHERE book_id = ?", arrayOf(bookId))
            .use { if (it.moveToFirst()) Remote(it.getDouble(0), it.getString(1), it.getString(2)) else null }

    /** Notes an offer as made; false when it was made before, so each position is offered once. */
    fun firstOffer(offer: String): Boolean =
        writableDatabase.insertWithOnConflict("asked", null, ContentValues().apply { put("offer", offer) }, SQLiteDatabase.CONFLICT_IGNORE) != -1L

    /** A book the app put on the Tolino that is still there. */
    fun delivered(bookId: String): Delivered? = deliveredWhere("book_id = ? AND removed = 0", bookId)

    fun deliveredAt(path: String): Delivered? = deliveredWhere("path = ?", path)

    fun onDeviceIds(): Set<String> = ids("SELECT book_id FROM delivered WHERE removed = 0")

    /** The books on the Tolino, the latest first. */
    fun onDevice(): List<BookSummary> =
        readableDatabase.rawQuery("SELECT * FROM delivered WHERE removed = 0 ORDER BY at DESC", null).use { c ->
            generateSequence { if (c.moveToNext()) summary(c) else null }.toList()
        }

    fun summary(bookId: String): BookSummary? =
        readableDatabase.rawQuery("SELECT * FROM delivered WHERE book_id = ?", arrayOf(bookId)).use {
            if (it.moveToFirst()) summary(it) else null
        }

    fun addDelivered(d: Delivered, book: BookSummary) {
        writableDatabase.insertWithOnConflict("delivered", null, ContentValues().apply {
            put("book_id", d.bookId)
            put("file_id", d.fileId)
            put("path", d.path)
            put("at", System.currentTimeMillis())
            put("removed", 0)
            put("title", book.title)
            put("authors", book.authors.joinToString(SEP))
            put("series", book.series)
            put("series_index", book.seriesIndex)
            put("formats", book.formats.joinToString(SEP))
        }, SQLiteDatabase.CONFLICT_REPLACE)
        report(d.bookId, true)
    }

    /** Notes the books whose files were deleted on the Tolino, which deletes them from its library. */
    fun noteRemoved() {
        val gone = readableDatabase.rawQuery("SELECT book_id, path FROM delivered WHERE removed = 0", null).use { c ->
            generateSequence { if (c.moveToNext()) c.getString(0) to c.getString(1) else null }
                .filter { !File(it.second).exists() }
                .map { it.first }
                .toList()
        }
        for (id in gone) {
            writableDatabase.execSQL("UPDATE delivered SET removed = 1 WHERE book_id = ?", arrayOf(id))
            report(id, false)
        }
    }

    fun saveProgress(d: Delivered, fraction: Double, at: Long) {
        writableDatabase.insertWithOnConflict("progress", null, ContentValues().apply {
            put("book_id", d.bookId)
            put("file_id", d.fileId)
            put("fraction", fraction)
            put("at", at)
            put("sent", 0)
        }, SQLiteDatabase.CONFLICT_REPLACE)
    }

    fun unsent(): List<Unsent> =
        readableDatabase.rawQuery("SELECT book_id, file_id, fraction, at FROM progress WHERE sent = 0", null)
            .use { c ->
                generateSequence {
                    if (c.moveToNext()) Unsent(c.getString(0), c.getString(1), c.getDouble(2), c.getLong(3)) else null
                }.toList()
            }

    /** Marks a position sent, unless a page was turned since it was read. */
    fun markSent(u: Unsent) {
        writableDatabase.execSQL(
            "UPDATE progress SET sent = 1 WHERE book_id = ? AND at = ?", arrayOf<Any>(u.bookId, u.at),
        )
    }

    private fun ids(sql: String): Set<String> =
        readableDatabase.rawQuery(sql, null).use { c ->
            generateSequence { if (c.moveToNext()) c.getString(0) else null }.toSet()
        }

    private fun deliveredWhere(where: String, arg: String): Delivered? =
        readableDatabase.rawQuery("SELECT book_id, file_id, path FROM delivered WHERE $where", arrayOf(arg)).use {
            if (it.moveToFirst()) Delivered(it.getString(0), it.getString(1), it.getString(2)) else null
        }

    private fun summary(c: Cursor) = BookSummary(
        id = c.getString(c.getColumnIndexOrThrow("book_id")),
        title = c.getString(c.getColumnIndexOrThrow("title")),
        authors = c.getString(c.getColumnIndexOrThrow("authors")).split(SEP).filter { it.isNotEmpty() },
        series = c.getString(c.getColumnIndexOrThrow("series")),
        seriesIndex = c.getColumnIndexOrThrow("series_index").let { if (c.isNull(it)) null else c.getDouble(it) },
        formats = c.getString(c.getColumnIndexOrThrow("formats")).split(SEP).filter { it.isNotEmpty() },
        addedAt = "",
    )

    companion object {
        private const val SEP = "\u001f"
    }
}
