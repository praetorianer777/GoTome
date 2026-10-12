package io.github.praetorianer777.gotome.tolino

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class JsonTest {
    @Test
    fun readsAPageOfBooks() {
        val (books, next) = Json.bookList(
            """{"books":[{"id":"0192f0a0-0000-7000-8000-0000000000b1","libraryId":"l","title":"Der Weitseher","authors":["Robin Hobb"],
               "series":"Fitz","seriesIndex":1,"formats":["epub","mobi"],"addedAt":"2026-10-01T10:00:00Z",
               "status":"unread","placeholder":false}],"nextCursor":"c2"}""",
        )
        assertEquals("c2", next)
        assertEquals(BookSummary("0192f0a0-0000-7000-8000-0000000000b1", "Der Weitseher", listOf("Robin Hobb"), "Fitz", 1.0, listOf("epub", "mobi"), "2026-10-01T10:00:00Z"), books.single())
    }

    @Test
    fun readsSearchHitsAndTheCount() {
        val hits = Json.searchHits(
            """{"books":[{"id":"0192f0a0-0000-7000-8000-0000000000b2","libraryId":"l","title":"Der Schattenbote","authors":["Robin Hobb"],
               "formats":["epub"],"addedAt":"2026-10-01T10:00:00Z","match":"title","placeholder":false}]}""",
        )
        assertEquals(listOf("0192f0a0-0000-7000-8000-0000000000b2"), hits.map { it.id })
        assertNull(hits.single().seriesIndex)
        assertEquals(49_731, Json.count("""{"count":49731}"""))
    }

    @Test
    fun describesADetailedBookByItsFiles() {
        val book = Json.bookDetail(
            """{"id":"0192f0a0-0000-7000-8000-0000000000b1","title":"T","series":"S","seriesIndex":2.5,"contributors":[{"name":"A","role":"author"}],
               "files":[{"id":"0192f0a0-0000-7000-8000-0000000000f1","format":"epub","drm":false,"missing":false,"size":1},
                        {"id":"0192f0a0-0000-7000-8000-0000000000f2","format":"EPUB","drm":false,"missing":false,"size":1},
                        {"id":"0192f0a0-0000-7000-8000-0000000000f3","format":"pdf","drm":false,"missing":false,"size":1}]}""",
        )
        assertEquals(BookSummary("0192f0a0-0000-7000-8000-0000000000b1", "T", listOf("A"), "S", 2.5, listOf("epub", "pdf"), ""), Json.summary(book))
    }

    @Test
    fun leavesOutWhatItCannotReadRatherThanTheList() {
        val (books, _) = Json.bookList(
            """{"books":[{"id":"../../auth/devices","title":"Escape","formats":[]},
                         {"id":"0192f0a0-0000-7000-8000-0000000000b2"},
                         7,
                         {"id":"0192f0a0-0000-7000-8000-0000000000b1","title":"Kept","formats":["epub"]}]}""",
        )
        assertEquals(listOf("Kept"), books.map { it.title })
        val book = Json.bookDetail(
            """{"id":"0192f0a0-0000-7000-8000-0000000000b1","title":"T",
               "files":[{"id":"x/y","format":"epub"},{"id":"0192f0a0-0000-7000-8000-0000000000f1","format":"epub"}]}""",
        )
        assertEquals(listOf("0192f0a0-0000-7000-8000-0000000000f1"), book.files.map { it.id })
        assertEquals(false, Json.isId("0192F0A0-0000-7000-8000-0000000000B1x"))
    }

    @Test
    fun readsWhomTheTokenSignsIn() {
        assertEquals(
            ThisDevice("Tolino", "steve"),
            Json.thisDevice("""{"device":{"id":"0192f0a0-0000-7000-8000-0000000000d1","name":"Tolino","createdAt":"t"},"username":"steve"}"""),
        )
    }

    @Test
    fun signsInWithBasicAuth() {
        assertEquals("Basic c3RldmU6QUJDRC1FRkdI", Api.basic("steve", "ABCD-EFGH"))
    }

    @Test
    fun theLastPageHasNoCursor() {
        assertNull(Json.bookList("""{"books":[]}""").second)
    }

    @Test
    fun picksAnEpubWithoutDrmBeforeAPdf() {
        val book = Json.bookDetail(
            """{"id":"0192f0a0-0000-7000-8000-0000000000b1","title":"T","contributors":[{"name":"A","role":"author"},{"name":"N","role":"narrator"}],
               "files":[{"id":"0192f0a0-0000-7000-8000-0000000000f1","format":"EPUB","drm":true,"missing":false,"size":1},
                        {"id":"0192f0a0-0000-7000-8000-0000000000f2","format":"pdf","drm":false,"missing":false,"size":2},
                        {"id":"0192f0a0-0000-7000-8000-0000000000f3","format":"epub","drm":false,"missing":true,"size":3},
                        {"id":"0192f0a0-0000-7000-8000-0000000000f4","format":"epub","drm":false,"missing":false,"size":4}]}""",
        )
        assertEquals(listOf("A"), book.authors)
        assertEquals("0192f0a0-0000-7000-8000-0000000000f4", Json.readableFile(book)?.id)
        assertEquals("0192f0a0-0000-7000-8000-0000000000f2", Json.readableFile(book.copy(files = book.files.filter { it.id != "0192f0a0-0000-7000-8000-0000000000f4" }))?.id)
        assertNull(Json.readableFile(book.copy(files = book.files.take(1))))
    }

    @Test
    fun sendsAPositionTheWebReaderOpens() {
        val update = JSONObject(Json.progressUpdate("0192f0a0-0000-7000-8000-0000000000f1", 0.25, "tolino-ab12cd34"))
        assertEquals("fraction:0.25", update.getString("locator"))
        assertEquals(0.25, update.getDouble("fraction"), 0.0)
        assertEquals("0192f0a0-0000-7000-8000-0000000000f1", update.getString("fileId"))
        assertEquals("tolino-ab12cd34", update.getString("clientId"))
    }

    @Test
    fun readsWhetherGotomeKeptThePosition() {
        assertEquals(Json.Saved(true, "tolino-1"), Json.progressSaved("""{"saved":true,"progress":{"clientId":"tolino-1","locator":"x","fraction":0.1,"updatedAt":"t"}}"""))
        assertEquals(Json.Saved(false, "web"), Json.progressSaved("""{"saved":false,"progress":{"clientId":"web","locator":"x","fraction":0.9,"updatedAt":"t"}}"""))
        assertEquals(Json.Saved(false, null), Json.progressSaved("""{"saved":false,"progress":null}"""))
        assertEquals(true, JSONObject(Json.progressUpdate("f", 0.1, "c", force = true)).getBoolean("force"))
        assertEquals(false, JSONObject(Json.progressUpdate("f", 0.1, "c")).has("force"))
    }

    @Test
    fun readsTheEbookPositionAndItsAbsence() {
        val p = Json.ebookProgress("""{"finishes":0,"ebook":{"locator":"epubcfi(/6/4)","fraction":0.47,"clientId":"web","updatedAt":"2026-10-10T08:00:00Z"}}""")
        assertEquals(0.47, p!!.fraction, 0.0)
        assertEquals("web", p.clientId)
        assertNull(Json.ebookProgress("""{"finishes":0}"""))
    }

    @Test
    fun readsTheErrorMessage() {
        assertEquals("Wrong user name or password", Json.errorMessage("""{"error":{"code":"unauthorized","message":"Wrong user name or password"}}"""))
        assertNull(Json.errorMessage("<html>"))
    }
}
