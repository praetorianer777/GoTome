package io.github.praetorianer777.gotome.tolino

import android.content.Context
import android.graphics.Bitmap
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Typeface
import java.io.ByteArrayOutputStream
import java.io.File
import java.util.zip.CRC32
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream

/**
 * A small book "GOtome" in the Tolino library. Opening it is how the store is
 * reached from the Tolino's home screen: the watcher sees it opened and brings
 * the store up over it.
 */
object LauncherBook {
    fun ensure(context: Context) {
        val file = File(Storage.LAUNCHER)
        if (file.exists()) return
        file.parentFile?.mkdirs()
        val part = File(file.parentFile, ".${file.name}.part")
        ZipOutputStream(part.outputStream()).use { zip ->
            // EPUB wants the mimetype first and stored, not compressed.
            val mimetype = "application/epub+zip".toByteArray()
            zip.putNextEntry(ZipEntry("mimetype").apply {
                method = ZipEntry.STORED
                size = mimetype.size.toLong()
                crc = CRC32().apply { update(mimetype) }.value
            })
            zip.write(mimetype)
            zip.entry("META-INF/container.xml", CONTAINER)
            zip.entry("OEBPS/content.opf", OPF)
            zip.entry("OEBPS/toc.ncx", NCX)
            zip.entry("OEBPS/cover.xhtml", page("""<div style="text-align:center"><img src="cover.jpg" alt="GOtome" style="height:95%"/></div>"""))
            zip.entry("OEBPS/text.xhtml", page(context.getString(R.string.launcher_text).split("\n").joinToString("") { "<p>$it</p>" }))
            zip.putNextEntry(ZipEntry("OEBPS/cover.jpg"))
            zip.write(cover())
        }
        part.renameTo(file)
    }

    private fun ZipOutputStream.entry(name: String, text: String) {
        putNextEntry(ZipEntry(name))
        write(text.toByteArray())
    }

    private fun cover(): ByteArray {
        val bitmap = Bitmap.createBitmap(600, 800, Bitmap.Config.ARGB_8888)
        val canvas = Canvas(bitmap)
        canvas.drawColor(Color.WHITE)
        val paint = Paint(Paint.ANTI_ALIAS_FLAG).apply {
            color = Color.BLACK
            textAlign = Paint.Align.CENTER
            typeface = Typeface.create(Typeface.SANS_SERIF, Typeface.BOLD)
        }
        paint.style = Paint.Style.STROKE
        paint.strokeWidth = 12f
        canvas.drawRect(30f, 30f, 570f, 770f, paint)
        paint.style = Paint.Style.FILL
        paint.textSize = 110f
        canvas.drawText("GOtome", 300f, 380f, paint)
        paint.textSize = 54f
        paint.typeface = Typeface.SANS_SERIF
        canvas.drawText("Store", 300f, 470f, paint)
        return ByteArrayOutputStream().also { bitmap.compress(Bitmap.CompressFormat.JPEG, 90, it) }.toByteArray()
    }

    private fun page(body: String) =
        """<?xml version="1.0" encoding="UTF-8"?><html xmlns="http://www.w3.org/1999/xhtml"><head><title>GOtome</title></head><body>$body</body></html>"""

    private const val ID = "urn:uuid:9f0c7d2e-6b1a-4c55-8e8e-60706f746f6d"

    private const val CONTAINER =
        """<?xml version="1.0"?><container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>"""

    private const val OPF =
        """<?xml version="1.0" encoding="UTF-8"?><package xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="id"><metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title>GOtome</dc:title><dc:creator>GOtome</dc:creator><dc:language>de</dc:language><dc:identifier id="id">$ID</dc:identifier><meta name="cover" content="cover-image"/></metadata><manifest><item id="cover-image" href="cover.jpg" media-type="image/jpeg"/><item id="cover" href="cover.xhtml" media-type="application/xhtml+xml"/><item id="text" href="text.xhtml" media-type="application/xhtml+xml"/><item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/></manifest><spine toc="ncx"><itemref idref="cover"/><itemref idref="text"/></spine><guide><reference type="cover" title="Cover" href="cover.xhtml"/></guide></package>"""

    private const val NCX =
        """<?xml version="1.0" encoding="UTF-8"?><ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1"><head><meta name="dtb:uid" content="$ID"/></head><docTitle><text>GOtome</text></docTitle><navMap><navPoint id="n1" playOrder="1"><navLabel><text>GOtome</text></navLabel><content src="cover.xhtml"/></navPoint></navMap></ncx>"""
}
