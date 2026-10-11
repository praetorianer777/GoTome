package io.github.praetorianer777.gotome.tolino

import android.content.ContentValues
import android.content.Context
import android.database.Cursor
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper
import java.text.Normalizer

data class Delivered(val bookId: String, val fileId: String, val path: String)

data class Unsent(val bookId: String, val fileId: String, val fraction: Double, val at: Long)

/**
 * The catalogue kept on the device, so that browsing never waits on the network,
 * the books the app put on the Tolino, and positions not yet sent to GOtome.
 */
class Db(context: Context) : SQLiteOpenHelper(context, "gotome.db", null, 1) {
    enum class Order { ADDED, TITLE }

    override fun onCreate(db: SQLiteDatabase) {
        db.execSQL(
            """CREATE TABLE books (id TEXT PRIMARY KEY, title TEXT NOT NULL, authors TEXT NOT NULL,
               series TEXT NOT NULL, series_index REAL, formats TEXT NOT NULL, added_at TEXT NOT NULL,
               sort TEXT NOT NULL, search TEXT NOT NULL)""",
        )
        db.execSQL("CREATE INDEX books_sort ON books(sort)")
        db.execSQL("CREATE INDEX books_added ON books(added_at)")
        db.execSQL(
            """CREATE TABLE delivered (book_id TEXT PRIMARY KEY, file_id TEXT NOT NULL,
               path TEXT NOT NULL UNIQUE, at INTEGER NOT NULL, removed INTEGER NOT NULL DEFAULT 0)""",
        )
        db.execSQL(
            """CREATE TABLE progress (book_id TEXT PRIMARY KEY, file_id TEXT NOT NULL,
               fraction REAL NOT NULL, at INTEGER NOT NULL, sent INTEGER NOT NULL DEFAULT 0)""",
        )
    }

    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) = Unit

    fun replaceCatalogue(books: List<BookSummary>) {
        val db = writableDatabase
        db.beginTransaction()
        try {
            db.delete("books", null, null)
            for (b in books) {
                db.insert("books", null, ContentValues().apply {
                    put("id", b.id)
                    put("title", b.title)
                    put("authors", b.authors.joinToString(SEP))
                    put("series", b.series)
                    put("series_index", b.seriesIndex)
                    put("formats", b.formats.joinToString(SEP))
                    put("added_at", b.addedAt)
                    put("sort", plain(b.title))
                    put("search", plain(listOf(b.title, b.series).plus(b.authors).joinToString(" ")))
                })
            }
            db.setTransactionSuccessful()
        } finally {
            db.endTransaction()
        }
    }

    fun catalogue(words: String, onDevice: Boolean, order: Order, offset: Int, limit: Int): List<BookSummary> {
        val (where, args) = filter(words, onDevice)
        val by = if (order == Order.ADDED) "added_at DESC" else "sort ASC"
        return readableDatabase.rawQuery(
            "SELECT * FROM books $where ORDER BY $by LIMIT $limit OFFSET $offset", args,
        ).use { c -> generateSequence { if (c.moveToNext()) summary(c) else null }.toList() }
    }

    fun count(words: String, onDevice: Boolean): Int {
        val (where, args) = filter(words, onDevice)
        return readableDatabase.rawQuery("SELECT count(*) FROM books $where", args).use {
            it.moveToFirst()
            it.getInt(0)
        }
    }

    fun book(id: String): BookSummary? =
        readableDatabase.rawQuery("SELECT * FROM books WHERE id = ?", arrayOf(id)).use {
            if (it.moveToFirst()) summary(it) else null
        }

    /** A book the app put on the Tolino that is still there. */
    fun delivered(bookId: String): Delivered? = deliveredWhere("book_id = ? AND removed = 0", bookId)

    fun deliveredAt(path: String): Delivered? = deliveredWhere("path = ?", path)

    /**
     * Every book the app ever put on the Tolino, also those deleted there since:
     * a book deleted on the device is not brought back by the next sync.
     */
    fun deliveredIds(): Set<String> = ids("SELECT book_id FROM delivered")

    fun onDeviceIds(): Set<String> = ids("SELECT book_id FROM delivered WHERE removed = 0")

    /** Notes the books whose files were deleted on the Tolino, which deletes them from its library. */
    fun noteRemoved() {
        val gone = readableDatabase.rawQuery("SELECT book_id, path FROM delivered WHERE removed = 0", null).use { c ->
            generateSequence { if (c.moveToNext()) c.getString(0) to c.getString(1) else null }
                .filter { !java.io.File(it.second).exists() }
                .map { it.first }
                .toList()
        }
        for (id in gone) writableDatabase.execSQL("UPDATE delivered SET removed = 1 WHERE book_id = ?", arrayOf(id))
    }

    private fun ids(sql: String): Set<String> =
        readableDatabase.rawQuery(sql, null).use { c ->
            generateSequence { if (c.moveToNext()) c.getString(0) else null }.toSet()
        }

    fun addDelivered(d: Delivered) {
        writableDatabase.insertWithOnConflict("delivered", null, ContentValues().apply {
            put("book_id", d.bookId)
            put("file_id", d.fileId)
            put("path", d.path)
            put("at", System.currentTimeMillis())
            put("removed", 0)
        }, SQLiteDatabase.CONFLICT_REPLACE)
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

    private fun deliveredWhere(where: String, arg: String): Delivered? =
        readableDatabase.rawQuery("SELECT book_id, file_id, path FROM delivered WHERE $where", arrayOf(arg)).use {
            if (it.moveToFirst()) Delivered(it.getString(0), it.getString(1), it.getString(2)) else null
        }

    private fun filter(words: String, onDevice: Boolean): Pair<String, Array<String>> {
        val terms = plain(words).split(' ').filter { it.isNotEmpty() }
        val clauses = terms.map { "search LIKE ?" } +
            (if (onDevice) listOf("id IN (SELECT book_id FROM delivered WHERE removed = 0)") else emptyList())
        val where = if (clauses.isEmpty()) "" else "WHERE " + clauses.joinToString(" AND ")
        return where to terms.map { "%$it%" }.toTypedArray()
    }

    private fun summary(c: Cursor) = BookSummary(
        id = c.getString(c.getColumnIndexOrThrow("id")),
        title = c.getString(c.getColumnIndexOrThrow("title")),
        authors = c.getString(c.getColumnIndexOrThrow("authors")).split(SEP).filter { it.isNotEmpty() },
        series = c.getString(c.getColumnIndexOrThrow("series")),
        seriesIndex = c.getColumnIndexOrThrow("series_index").let { if (c.isNull(it)) null else c.getDouble(it) },
        formats = c.getString(c.getColumnIndexOrThrow("formats")).split(SEP).filter { it.isNotEmpty() },
        addedAt = c.getString(c.getColumnIndexOrThrow("added_at")),
    )

    companion object {
        private const val SEP = "\u001f"
    }
}

/** Lower case without accents, so that "Muller" finds "Müller". */
fun plain(text: String): String =
    Normalizer.normalize(text.lowercase().replace("ß", "ss"), Normalizer.Form.NFD)
        .replace(Regex("\\p{M}+"), "")
        .replace(Regex("[^\\p{L}\\p{N}]+"), " ")
        .trim()
