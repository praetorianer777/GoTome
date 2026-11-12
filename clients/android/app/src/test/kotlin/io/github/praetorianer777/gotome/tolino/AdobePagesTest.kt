package io.github.praetorianer777.gotome.tolino

import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import java.util.zip.CRC32
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream

class AdobePagesTest {
    @get:Rule
    val dir = TemporaryFolder()

    @Test
    fun countsAPageForEvery1024CompressedBytesOfEachSpineDocument() {
        val book = epub(
            opfPath = "OEBPS/content.opf",
            opf = """<package><manifest>
                <item id="c1" href="Text/one.xhtml" media-type="application/xhtml+xml"/>
                <item href="Text/two%20b.xhtml" id="c2" media-type="application/xhtml+xml"/>
                <item id="css" href="style.css" media-type="text/css"/>
                <item id="c3" href="../extra.xhtml" media-type="application/xhtml+xml"/>
                </manifest><spine><itemref idref="c1"/><itemref idref="c2"/><itemref idref="c3"/></spine></package>""",
            stored = mapOf(
                "OEBPS/Text/one.xhtml" to 3000,
                "OEBPS/Text/two b.xhtml" to 1024,
                "OEBPS/style.css" to 9000,
                "extra.xhtml" to 10,
            ),
        )
        assertEquals(3 + 1 + 1, AdobePages.count(book))
    }

    @Test
    fun aBrokenBookHasNoPages() {
        val file = dir.newFile("broken.epub")
        ZipOutputStream(file.outputStream()).use { it.putNextEntry(ZipEntry("mimetype")) }
        assertEquals(0, AdobePages.count(file))
    }

    private fun epub(opfPath: String, opf: String, stored: Map<String, Int>): File {
        val file = dir.newFile("book.epub")
        ZipOutputStream(file.outputStream()).use { zip ->
            zip.putNextEntry(ZipEntry("META-INF/container.xml"))
            zip.write("""<container><rootfiles><rootfile full-path="$opfPath"/></rootfiles></container>""".toByteArray())
            zip.putNextEntry(ZipEntry(opfPath))
            zip.write(opf.toByteArray())
            // Stored, so that the compressed size is the size.
            for ((name, size) in stored) {
                val bytes = ByteArray(size) { 'x'.code.toByte() }
                zip.putNextEntry(ZipEntry(name).apply {
                    method = ZipEntry.STORED
                    this.size = size.toLong()
                    compressedSize = size.toLong()
                    crc = CRC32().apply { update(bytes) }.value
                })
                zip.write(bytes)
            }
        }
        return file
    }
}
