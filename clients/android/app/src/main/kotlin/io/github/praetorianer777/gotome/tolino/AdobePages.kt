package io.github.praetorianer777.gotome.tolino

import java.io.File
import java.net.URLDecoder
import java.util.zip.ZipFile

/**
 * How many pages Adobe's reader engine, which the Tolino reads EPUBs with, counts
 * in a book: one for every 1024 bytes of each spine document as compressed in
 * the EPUB, at least one each. The reader logs its position in these pages, so
 * this turns that position into a share of the book. Checked on a Shine 4: the
 * logged positions cross from one document into the next exactly at these sums.
 */
object AdobePages {
    private val rootfile = Regex("""full-path\s*=\s*"([^"]+)"""")
    private val item = Regex("""<(?:\w+:)?item\b[^>]*>""")
    private val itemref = Regex("""<(?:\w+:)?itemref\b[^>]*\bidref\s*=\s*"([^"]+)"""")

    fun count(file: File): Int = ZipFile(file).use { zip ->
        val container = zip.getEntry("META-INF/container.xml")?.let { zip.getInputStream(it).reader().readText() }
            ?: return 0
        val opfPath = rootfile.find(container)?.groupValues?.get(1) ?: return 0
        val opf = zip.getInputStream(zip.getEntry(opfPath) ?: return 0).reader().readText()
        val base = opfPath.substringBeforeLast('/', "")
        val hrefs = item.findAll(opf).mapNotNull { tag ->
            val id = attribute(tag.value, "id") ?: return@mapNotNull null
            val href = attribute(tag.value, "href") ?: return@mapNotNull null
            id to href
        }.toMap()
        itemref.findAll(opf).sumOf { ref ->
            val href = hrefs[ref.groupValues[1]] ?: return@sumOf 0
            val entry = zip.getEntry(resolve(base, URLDecoder.decode(href.substringBefore('#'), "UTF-8")))
            if (entry == null) 0 else maxOf(1, ((entry.compressedSize + 1023) / 1024).toInt())
        }
    }

    private fun attribute(tag: String, name: String): String? =
        Regex("""\b$name\s*=\s*"([^"]*)"""").find(tag)?.groupValues?.get(1)

    private fun resolve(base: String, href: String): String {
        val parts = ArrayDeque<String>()
        for (part in (if (base.isEmpty()) href else "$base/$href").split('/')) when (part) {
            "", "." -> Unit
            ".." -> parts.removeLastOrNull()
            else -> parts.addLast(part)
        }
        return parts.joinToString("/")
    }
}
