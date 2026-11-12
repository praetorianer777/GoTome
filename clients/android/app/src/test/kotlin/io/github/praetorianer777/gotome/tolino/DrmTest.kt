package io.github.praetorianer777.gotome.tolino

import org.junit.Assert.assertEquals
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream

class DrmTest {
    @get:Rule val tmp = TemporaryFolder()

    private val fonts = """<?xml version="1.0"?>
        <encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
          <EncryptedData xmlns="http://www.w3.org/2001/04/xmlenc#">
            <EncryptionMethod Algorithm="http://www.idpf.org/2008/embedding"/>
            <CipherData><CipherReference URI="OEBPS/fonts/a.otf"/></CipherData>
          </EncryptedData>
          <EncryptedData xmlns="http://www.w3.org/2001/04/xmlenc#">
            <EncryptionMethod Algorithm='http://ns.adobe.com/pdf/enc'/>
            <CipherData><CipherReference URI="OEBPS/fonts/b.otf"/></CipherData>
          </EncryptedData>
        </encryption>"""

    private val adept = """<?xml version="1.0"?>
        <encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
          <enc:EncryptedData>
            <enc:EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes128-cbc"/>
            <enc:CipherData><enc:CipherReference URI="OEBPS/ch1.xhtml"/></enc:CipherData>
          </enc:EncryptedData>
        </encryption>"""

    private fun epub(name: String, entries: Map<String, String>): File {
        val file = tmp.newFile(name)
        ZipOutputStream(file.outputStream()).use { zip ->
            for ((n, text) in entries) {
                zip.putNextEntry(ZipEntry(n))
                zip.write(text.toByteArray())
                zip.closeEntry()
            }
        }
        return file
    }

    @Test
    fun anEpubWithoutAnEncryptionFileHasNoDrm() {
        assertEquals(false, Drm.epub(epub("plain.epub", mapOf("mimetype" to "application/epub+zip"))))
    }

    @Test
    fun obfuscatedFontsAreNoDrm() {
        assertEquals(false, Drm.encryptionIsDrm(fonts, hasRights = false))
        assertEquals(false, Drm.epub(epub("fonts.epub", mapOf("META-INF/encryption.xml" to fonts))))
    }

    @Test
    fun encryptedChaptersAreDrm() {
        assertEquals(true, Drm.encryptionIsDrm(adept, hasRights = false))
        assertEquals(true, Drm.epub(epub("locked.epub", mapOf("META-INF/encryption.xml" to adept))))
    }

    @Test
    fun anAdeptKeyIsDrmEvenBesideFonts() {
        val keyed = fonts.replace("</encryption>", """<enc:EncryptedKey xmlns:enc="http://www.w3.org/2001/04/xmlenc#"/></encryption>""")
        assertEquals(true, Drm.encryptionIsDrm(keyed, hasRights = false))
        assertEquals(true, Drm.encryptionIsDrm(fonts.replace("</encryption>", "<!-- Adobe ADEPT --></encryption>"), hasRights = false))
    }

    @Test
    fun aRightsFileIsDrm() {
        assertEquals(true, Drm.epub(epub("rights.epub", mapOf("META-INF/rights.xml" to "<rights/>"))))
    }

    @Test
    fun aFileThatIsNoZipCountsAsProtected() {
        val broken = tmp.newFile("broken.epub").apply { writeText("not a zip") }
        assertEquals(true, Drm.epub(broken))
    }

    @Test
    fun aPdfNamingAnEncryptionDictionaryInItsTrailerIsDrm() {
        val trailer = "trailer\n<< /Size 9 /Root 1 0 R /Encrypt 8 0 R /ID [<aa><bb>] >>\nstartxref\n1234\n%%EOF"
        assertEquals(true, Drm.pdfEncrypted(ByteArray(0), trailer.toByteArray()))
        val xrefStream = "5 0 obj\n<< /Type /XRef /Size 9 /Encrypt 8 0 R /W [1 2 1] >>\nstream"
        assertEquals(true, Drm.pdfEncrypted(xrefStream.toByteArray(), ByteArray(0)))
    }

    @Test
    fun aPlainPdfHasNoDrm() {
        val pdf = tmp.newFile("plain.pdf").apply {
            writeText("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\ntrailer\n<< /Size 2 /Root 1 0 R >>\nstartxref\n9\n%%EOF")
        }
        assertEquals(false, Drm.pdf(pdf))
        val locked = tmp.newFile("locked.pdf").apply {
            writeText("%PDF-1.7\n" + "x".repeat(20000) + "\ntrailer\n<< /Size 2 /Encrypt 3 0 R >>\nstartxref\n9\n%%EOF")
        }
        assertEquals(true, Drm.pdf(locked))
    }
}
