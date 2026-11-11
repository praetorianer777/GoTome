package io.github.praetorianer777.gotome.tolino

import android.content.ContentValues
import android.content.Context
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper

/**
 * What the app keeps of the Tolino's own books: the hash of each file by path,
 * size and modification time, so that a file is read once, and which of them
 * GOtome has a book for. Kept apart from [Db], whose books are the app's own.
 */
class OwnDb(context: Context) : SQLiteOpenHelper(context, "own.db", null, 1) {
    override fun onCreate(db: SQLiteDatabase) {
        db.execSQL(
            """CREATE TABLE files (path TEXT PRIMARY KEY, size INTEGER NOT NULL, modified INTEGER NOT NULL,
               sha256 TEXT, drm INTEGER NOT NULL, book_id TEXT, file_id TEXT)""",
        )
    }

    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) = Unit

    fun files(): List<OwnFile> = query("SELECT path, size, modified, sha256, drm FROM files") { c ->
        OwnFile(c.getString(0), c.getLong(1), c.getLong(2), c.getString(3), c.getInt(4) == 1)
    }

    /** Replaces what is known with the files found now, keeping what GOtome said of those unchanged. */
    fun saveFiles(files: List<OwnFile>) {
        val db = writableDatabase
        db.beginTransaction()
        try {
            val matched = query("SELECT path, sha256, book_id, file_id FROM files WHERE book_id IS NOT NULL") { c ->
                c.getString(0) to Triple(c.getString(1), c.getString(2), c.getString(3))
            }.toMap()
            db.delete("files", null, null)
            for (f in files) {
                val kept = matched[f.path]?.takeIf { it.first == f.sha256 }
                db.insert("files", null, ContentValues().apply {
                    put("path", f.path)
                    put("size", f.size)
                    put("modified", f.modified)
                    put("sha256", f.sha256)
                    put("drm", if (f.drm) 1 else 0)
                    put("book_id", kept?.second)
                    put("file_id", kept?.third)
                })
            }
            db.setTransactionSuccessful()
        } finally {
            db.endTransaction()
        }
    }

    /** Records which files GOtome has a book for; the others have none. */
    fun saveMatches(files: List<OwnFile>, matches: List<OwnMatch>) {
        val db = writableDatabase
        db.beginTransaction()
        try {
            db.execSQL("UPDATE files SET book_id = NULL, file_id = NULL")
            val byHash = matches.associateBy { it.sha256 }
            for (f in files) {
                val m = f.sha256?.let(byHash::get) ?: continue
                setMatch(f.path, m)
            }
            db.setTransactionSuccessful()
        } finally {
            db.endTransaction()
        }
    }

    fun setMatch(path: String, match: OwnMatch) {
        writableDatabase.execSQL(
            "UPDATE files SET book_id = ?, file_id = ? WHERE path = ?", arrayOf(match.bookId, match.fileId, path),
        )
    }

    /** The book in a file of the Tolino's own, as the reader's log names it by path. */
    fun matchedAt(path: String): Delivered? =
        query("SELECT book_id, file_id, path FROM files WHERE path = ? AND book_id IS NOT NULL", path) {
            Delivered(it.getString(0), it.getString(1), it.getString(2))
        }.firstOrNull()

    fun matchedBookIds(): Set<String> =
        query("SELECT DISTINCT book_id FROM files WHERE book_id IS NOT NULL") { it.getString(0) }.toSet()

    /** Files without DRM that GOtome has no book for, by name. */
    fun unmatched(): List<OwnFile> = query(
        "SELECT path, size, modified, sha256, drm FROM files WHERE drm = 0 AND sha256 IS NOT NULL AND book_id IS NULL ORDER BY path",
    ) { c -> OwnFile(c.getString(0), c.getLong(1), c.getLong(2), c.getString(3), false) }

    private fun <T> query(sql: String, vararg args: String, read: (android.database.Cursor) -> T): List<T> =
        readableDatabase.rawQuery(sql, args.takeIf { it.isNotEmpty() }).use { c ->
            generateSequence { if (c.moveToNext()) read(c) else null }.toList()
        }
}
