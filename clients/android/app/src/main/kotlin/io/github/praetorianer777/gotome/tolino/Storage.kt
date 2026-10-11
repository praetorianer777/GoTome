package io.github.praetorianer777.gotome.tolino

import java.text.Normalizer

/** Where books go on the Tolino and what they are called there. */
object Storage {
    /** The reader app imports what appears under Books/ by itself, subfolders included. */
    const val ROOT = "/storage/emulated/0"
    const val BOOKS = "$ROOT/Books"
    const val DELIVERED = "$BOOKS/GOtome"
    const val LAUNCHER = "$BOOKS/GOtome.epub"

    /** The formats the reader app opens, the one to prefer first. */
    val READABLE = listOf("epub", "pdf")

    /** The same file under the one name the reader app uses for it. */
    fun canonical(path: String): String = when {
        path.startsWith("/sdcard/") -> ROOT + path.removePrefix("/sdcard")
        path.startsWith("/mnt/sdcard/") -> ROOT + path.removePrefix("/mnt/sdcard")
        else -> path
    }

    /**
     * A file name of plain letters, digits, dashes and underscores, so that no log
     * line or URL ever escapes it. The book ID's start keeps two books apart that
     * share an author and a title.
     */
    fun fileName(bookId: String, title: String, authors: List<String>, format: String): String {
        val author = slug(authors.firstOrNull().orEmpty(), 30)
        val name = listOf(author, slug(title, 60), bookId.replace("-", "").take(8))
            .filter { it.isNotEmpty() }
            .joinToString("-")
        return "$name.${format.lowercase()}"
    }

    fun slug(text: String, max: Int): String {
        val plain = Normalizer.normalize(text.replace("ß", "ss"), Normalizer.Form.NFD)
            .replace(Regex("\\p{M}+"), "")
        return plain.replace(Regex("[^A-Za-z0-9]+"), "_")
            .trim('_')
            .take(max)
            .trimEnd('_')
    }
}
