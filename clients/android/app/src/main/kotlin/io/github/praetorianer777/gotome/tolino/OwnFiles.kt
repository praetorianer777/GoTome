package io.github.praetorianer777.gotome.tolino

import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.security.MessageDigest

/** A book file on the Tolino that the app did not put there, with what is known of it. */
data class OwnFile(
    val path: String,
    val size: Long,
    val modified: Long,
    /** Hex SHA-256; null for a file with DRM, which is not hashed. */
    val sha256: String?,
    val drm: Boolean,
)

/** The book a hash found in GOtome, and the file of it that has the hash. */
data class OwnMatch(val sha256: String, val bookId: String, val fileId: String)

/** What GOtome says of uploading from this device now; [reason] is token, role, library or quota. */
data class UploadState(val allowed: Boolean, val reason: String?, val library: String?)

/**
 * The Tolino's own EPUBs and PDFs: found under Books/, hashed once, and told
 * apart from the books GOtome has.
 */
object OwnFiles {
    private val FORMATS = setOf("epub", "pdf")

    /**
     * The EPUBs and PDFs under [books], except those in [delivered] (the app's own
     * folder), the launcher book, and anything hidden: a dot name is a part
     * being written, or the reader's own.
     */
    fun list(books: File, delivered: File, launcher: File): List<File> {
        val skipDir = delivered.canonicalFile
        val skipFile = launcher.canonicalFile
        return books.walkTopDown()
            .onEnter { it.name.startsWith(".").not() && it.canonicalFile != skipDir }
            .filter { it.isFile && !it.name.startsWith(".") && it.canonicalFile != skipFile }
            .filter { it.extension.lowercase() in FORMATS }
            .sortedBy { it.path }
            .toList()
    }

    /**
     * Looks at each file once: a file whose path, size and modification time are
     * those in [known] keeps what was found then. Others are checked for DRM and
     * hashed, which reads them whole; [hash] and [isDrm] are separate so that
     * a file with DRM is never read to the end.
     */
    fun scan(
        files: List<File>,
        known: Map<String, OwnFile>,
        isDrm: (File) -> Boolean = ::drm,
        hash: (File) -> String = ::sha256,
        progress: (done: Int, total: Int) -> Unit = { _, _ -> },
    ): List<OwnFile> = files.mapIndexed { i, file ->
        progress(i, files.size)
        val size = file.length()
        val modified = file.lastModified()
        known[file.path]?.takeIf { it.size == size && it.modified == modified }
            ?: if (isDrm(file)) OwnFile(file.path, size, modified, null, true)
            else OwnFile(file.path, size, modified, hash(file), false)
    }

    fun drm(file: File): Boolean = if (file.extension.equals("pdf", ignoreCase = true)) Drm.pdf(file) else Drm.epub(file)

    fun sha256(file: File): String {
        val digest = MessageDigest.getInstance("SHA-256")
        file.inputStream().use { input ->
            val buffer = ByteArray(64 * 1024)
            while (true) {
                val n = input.read(buffer)
                if (n < 0) break
                digest.update(buffer, 0, n)
            }
        }
        return digest.digest().joinToString("") { "%02x".format(it) }
    }

    /** The files GOtome has a book for, and the ones without DRM it has none for. */
    fun split(files: List<OwnFile>, matches: List<OwnMatch>): Pair<List<Pair<OwnFile, OwnMatch>>, List<OwnFile>> {
        val byHash = matches.associateBy { it.sha256 }
        val found = mutableListOf<Pair<OwnFile, OwnMatch>>()
        val missing = mutableListOf<OwnFile>()
        for (file in files) {
            if (file.drm || file.sha256 == null) continue
            val match = byHash[file.sha256]
            if (match != null) found += file to match else missing += file
        }
        return found to missing
    }

    /** GOtome answers about 500 hashes at a time. */
    const val BATCH = 500

    fun matchesRequest(hashes: List<String>): String =
        JSONObject().put("hashes", JSONArray(hashes)).toString()

    fun matches(body: String): List<OwnMatch> {
        val array = JSONObject(body).getJSONArray("matches")
        return (0 until array.length()).mapNotNull { i ->
            val o = array.getJSONObject(i)
            val book = o.getString("bookId")
            val file = o.getString("fileId")
            val sha = o.getString("sha256").lowercase()
            if (Json.isId(book) && Json.isId(file) && sha.length == 64) OwnMatch(sha, book, file) else null
        }
    }

    fun uploadState(body: String): UploadState {
        val upload = JSONObject(body).getJSONObject("upload")
        return UploadState(
            allowed = upload.getBoolean("allowed"),
            reason = upload.optString("reason").ifEmpty { null },
            library = upload.optString("library").ifEmpty { null },
        )
    }

    /** The book an upload became, or the one that had the file already. */
    fun uploadedBook(body: String): String =
        JSONObject(body).getString("bookId").also { require(Json.isId(it)) { "not an ID: $it" } }
}
