package io.github.praetorianer777.gotome.tolino

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class JsonTest {
    @Test
    fun readsAPageOfBooks() {
        val (books, next) = Json.bookList(
            """{"books":[{"id":"b1","libraryId":"l","title":"Der Weitseher","authors":["Robin Hobb"],
               "series":"Fitz","seriesIndex":1,"formats":["epub","mobi"],"addedAt":"2026-10-01T10:00:00Z",
               "status":"unread","placeholder":false}],"nextCursor":"c2"}""",
        )
        assertEquals("c2", next)
        assertEquals(BookSummary("b1", "Der Weitseher", listOf("Robin Hobb"), "Fitz", 1.0, listOf("epub", "mobi"), "2026-10-01T10:00:00Z"), books.single())
    }

    @Test
    fun theLastPageHasNoCursor() {
        assertNull(Json.bookList("""{"books":[]}""").second)
    }

    @Test
    fun picksAnEpubWithoutDrmBeforeAPdf() {
        val book = Json.bookDetail(
            """{"id":"b1","title":"T","contributors":[{"name":"A","role":"author"},{"name":"N","role":"narrator"}],
               "files":[{"id":"f1","format":"EPUB","drm":true,"missing":false,"size":1},
                        {"id":"f2","format":"pdf","drm":false,"missing":false,"size":2},
                        {"id":"f3","format":"epub","drm":false,"missing":true,"size":3},
                        {"id":"f4","format":"epub","drm":false,"missing":false,"size":4}]}""",
        )
        assertEquals(listOf("A"), book.authors)
        assertEquals("f4", Json.readableFile(book)?.id)
        assertEquals("f2", Json.readableFile(book.copy(files = book.files.filter { it.id != "f4" }))?.id)
        assertNull(Json.readableFile(book.copy(files = book.files.take(1))))
    }

    @Test
    fun sendsAPositionTheWebReaderOpens() {
        val update = JSONObject(Json.progressUpdate("f1", 0.25, "tolino-ab12cd34"))
        assertEquals("fraction:0.25", update.getString("locator"))
        assertEquals(0.25, update.getDouble("fraction"), 0.0)
        assertEquals("f1", update.getString("fileId"))
        assertEquals("tolino-ab12cd34", update.getString("clientId"))
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
