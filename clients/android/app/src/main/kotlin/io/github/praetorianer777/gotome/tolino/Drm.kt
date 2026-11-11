package io.github.praetorianer777.gotome.tolino

import java.io.File
import java.io.RandomAccessFile
import java.util.zip.ZipFile

/**
 * Whether a book on the Tolino is protected, which GOtome neither reads nor is
 * offered. A file that cannot be read at all counts as protected: there is
 * nothing to hash or upload of it either way.
 */
object Drm {
    /** The two ways EPUBs obfuscate fonts, which is no protection of the book. */
    private val FONT_ALGORITHMS = setOf(
        "http://www.idpf.org/2008/embedding",
        "http://ns.adobe.com/pdf/enc",
    )

    private val ENCRYPTED_KEY = Regex("<([A-Za-z0-9_.-]+:)?EncryptedKey[\\s>/]")
    private val METHOD = Regex("<([A-Za-z0-9_.-]+:)?EncryptionMethod\\b[^>]*?\\bAlgorithm\\s*=\\s*(\"([^\"]*)\"|'([^']*)')")

    private const val MAX_XML = 1 shl 20
    private const val PDF_WINDOW = 8192

    fun epub(file: File): Boolean = try {
        ZipFile(file).use { zip ->
            val rights = zip.getEntry("META-INF/rights.xml") != null
            val xml = zip.getEntry("META-INF/encryption.xml")?.let { entry ->
                if (entry.size > MAX_XML) return true
                zip.getInputStream(entry).use { it.readBytes().toString(Charsets.UTF_8) }
            }
            encryptionIsDrm(xml, rights)
        }
    } catch (e: Exception) {
        true
    }

    /**
     * META-INF/encryption.xml lists what is encrypted. Adobe's ADEPT names a key
     * (EncryptedKey, with its rights.xml) and encrypts chapters, images and
     * styles; a font in an EPUB with nothing else encrypted is only obfuscated.
     */
    fun encryptionIsDrm(encryptionXml: String?, hasRights: Boolean): Boolean {
        if (hasRights) return true
        val xml = encryptionXml ?: return false
        if (ENCRYPTED_KEY.containsMatchIn(xml) || xml.contains("adept", ignoreCase = true)) return true
        return METHOD.findAll(xml).any { m ->
            val algorithm = m.groupValues[3].ifEmpty { m.groupValues[4] }
            algorithm !in FONT_ALGORITHMS
        }
    }

    fun pdf(file: File): Boolean = try {
        RandomAccessFile(file, "r").use { f ->
            val length = f.length()
            val head = ByteArray(minOf(length, PDF_WINDOW.toLong()).toInt()).also { f.readFully(it) }
            val tail = if (length <= PDF_WINDOW) head else {
                ByteArray(PDF_WINDOW).also {
                    f.seek(length - PDF_WINDOW)
                    f.readFully(it)
                }
            }
            pdfEncrypted(head, tail)
        }
    } catch (e: Exception) {
        true
    }

    /**
     * An encrypted PDF names its encryption dictionary as /Encrypt in the trailer
     * (or in the dictionary of a cross-reference stream), at the end of the file,
     * or at its start when it is linearized.
     */
    fun pdfEncrypted(head: ByteArray, tail: ByteArray): Boolean =
        mentionsEncrypt(head) || mentionsEncrypt(tail)

    private fun mentionsEncrypt(bytes: ByteArray): Boolean = String(bytes, Charsets.ISO_8859_1).contains("/Encrypt")
}
