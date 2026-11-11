package io.github.praetorianer777.gotome.tolino

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Rule
import org.junit.Test
import org.junit.rules.TemporaryFolder
import java.io.File

class OwnFilesTest {
    @get:Rule val tmp = TemporaryFolder()

    private fun answer(name: String) = javaClass.getResource("/api/$name.json")!!.readText()

    private fun file(dir: File, name: String, text: String): File =
        File(dir, name).apply { parentFile!!.mkdirs(); writeText(text) }

    @Test
    fun listsEpubsAndPdfsExceptTheAppsOwn() {
        val books = tmp.newFolder("Books")
        val delivered = File(books, "GOtome")
        val launcher = file(books, "GOtome.epub", "launcher")
        file(books, "a.epub", "a")
        file(books, "Sub/b.PDF", "b")
        file(books, "Sub/notes.txt", "n")
        file(books, "Sub/c.mobi", "m")
        file(delivered, "mine.epub", "m")
        file(books, ".hidden/d.epub", "d")
        file(books, ".e.epub.part", "e")

        val found = OwnFiles.list(books, delivered, launcher).map { it.relativeTo(books).path }
        assertEquals(listOf("Sub/b.PDF", "a.epub"), found)
    }

    @Test
    fun hashesAFileOnceWhilePathSizeAndTimeStay() {
        val books = tmp.newFolder("Books")
        val a = file(books, "a.epub", "first")
        val hashed = mutableListOf<String>()
        val hash = { f: File -> hashed += f.name; OwnFiles.sha256(f) }

        val first = OwnFiles.scan(listOf(a), emptyMap(), isDrm = { false }, hash = hash)
        assertEquals(listOf("a.epub"), hashed)
        assertEquals("a7937b64b8caa58f03721bb6bacf5c78cb235febe0e70b1b84cd99541461a08e", first[0].sha256)

        val again = OwnFiles.scan(listOf(a), first.associateBy { it.path }, isDrm = { false }, hash = hash)
        assertEquals("not read again", listOf("a.epub"), hashed)
        assertEquals(first, again)

        a.writeText("second!")
        val changed = OwnFiles.scan(listOf(a), first.associateBy { it.path }, isDrm = { false }, hash = hash)
        assertEquals(listOf("a.epub", "a.epub"), hashed)
        assertFalse(changed[0].sha256 == first[0].sha256)

        a.writeText("second!")
        a.setLastModified(a.lastModified() + 60_000)
        OwnFiles.scan(listOf(a), changed.associateBy { it.path }, isDrm = { false }, hash = hash)
        assertEquals("a new time is a new look", 3, hashed.size)
    }

    @Test
    fun aFileWithDrmIsNeverHashed() {
        val books = tmp.newFolder("Books")
        val a = file(books, "locked.epub", "locked")
        val scanned = OwnFiles.scan(listOf(a), emptyMap(), isDrm = { true }, hash = { error("read a file with DRM") })
        assertTrue(scanned[0].drm)
        assertNull(scanned[0].sha256)
    }

    @Test
    fun splitsTheFilesGOtomeHasFromThoseItLacks() {
        val book = "0192f0a0-0000-7000-8000-000000000001"
        val bookFile = "0192f0a0-0000-7000-8000-000000000002"
        val known = OwnFile("/b/known.epub", 1, 1, "aa".repeat(32), false)
        val twin = OwnFile("/b/copy of known.epub", 1, 1, "aa".repeat(32), false)
        val lacking = OwnFile("/b/new.epub", 1, 1, "bb".repeat(32), false)
        val locked = OwnFile("/b/locked.epub", 1, 1, null, true)
        val (found, missing) = OwnFiles.split(
            listOf(known, twin, lacking, locked),
            listOf(OwnMatch("aa".repeat(32), book, bookFile)),
        )
        assertEquals(listOf(known, twin), found.map { it.first })
        assertEquals(listOf(book, book), found.map { it.second.bookId })
        assertEquals("a file with DRM is neither matched nor offered", listOf(lacking), missing)
    }

    @Test
    fun readsGOtomesMatches() {
        val matches = OwnFiles.matches(answer("device-matches"))
        assertEquals(
            listOf(
                OwnMatch(
                    "433ba7969264d8618649d8fe920d412b691f4d506260fea48163ff91e9c25e4c",
                    "0192f0a0-0000-7000-8000-000000000001",
                    "0192f0a0-0000-7000-8000-000000000002",
                ),
            ),
            matches,
        )
    }

    @Test
    fun leavesOutAMatchWithoutRealIds() {
        val body = """{"matches":[{"sha256":"${"a".repeat(64)}","bookId":"../x","fileId":"y"}]}"""
        assertEquals(emptyList<OwnMatch>(), OwnFiles.matches(body))
    }

    @Test
    fun readsWhetherTheDeviceMayUpload() {
        assertEquals(UploadState(false, "token", null), OwnFiles.uploadState(answer("device")))
        val allowed = """{"upload":{"allowed":true,"library":"Shelf"}}"""
        assertEquals(UploadState(true, null, "Shelf"), OwnFiles.uploadState(allowed))
    }

    @Test
    fun asksAboutHashesAsJson() {
        assertEquals("""{"hashes":["aa","bb"]}""", OwnFiles.matchesRequest(listOf("aa", "bb")))
    }
}
