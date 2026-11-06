package io.github.praetorianer777.gotome.tolino

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

/**
 * Reads what GOtome answers the app. The files under resources/api are the
 * server's own answers, held to it by the backend's
 * TestTheDeviceClientsFixturesAreTheServersAnswers (make device-fixtures
 * writes them anew), with IDs numbered and times and secrets made fixed.
 */
class ServerAnswersTest {
    private fun answer(name: String) = javaClass.getResource("/api/$name.json")!!.readText()

    private val pride = "0192f0a0-0000-7000-8000-000000000001"
    private val prideEpub = "0192f0a0-0000-7000-8000-000000000002"
    private val schimmelreiter = "0192f0a0-0000-7000-8000-000000000003"
    private val time = "2026-10-11T12:00:00Z"

    @Test
    fun readsWhomTheTokenSignsIn() {
        assertEquals(ThisDevice("Tolino", "steve"), Json.thisDevice(answer("device")))
    }

    @Test
    fun readsWhatWasSentToTheDevice() {
        val (books, next) = Json.bookList(answer("device-books"))
        assertNull(next)
        assertEquals(
            listOf(
                BookSummary(pride, "Pride & Prejudice: A Novel", listOf("Jane Austen"), "Austen's Novels", 2.0, listOf("epub", "pdf"), time),
                BookSummary(schimmelreiter, "Der Schimmelreiter", emptyList(), "", null, listOf("azw3"), time),
            ),
            books,
        )
    }

    @Test
    fun readsAPageOfTheLibraryAndTheCount() {
        val (books, next) = Json.bookList(answer("books-page"))
        assertEquals(listOf("Emma"), books.map { it.title })
        assertEquals("nextCursor-drawn-at-random", next)
        assertEquals(2, Json.count(answer("books-count")))
    }

    @Test
    fun readsSearchHits() {
        assertEquals(listOf(pride), Json.searchHits(answer("books-search")).map { it.id })
    }

    @Test
    fun readsABookAndPicksItsEpub() {
        val book = Json.bookDetail(answer("book"))
        assertEquals(listOf("Jane Austen"), book.authors)
        assertEquals(2.0, book.seriesIndex!!, 0.0)
        assertEquals(listOf("epub", "pdf"), book.files.map { it.format })
        assertEquals(prideEpub, Json.readableFile(book)?.id)
        assertEquals(listOf("epub", "pdf"), Json.summary(book).formats)
    }

    @Test
    fun readsThePositionAndWhetherItWasKept() {
        assertNull(Json.ebookProgress(answer("progress-none")))
        assertEquals(Json.Saved(true, "tolino-ab12cd34"), Json.progressSaved(answer("progress-saved")))
        assertEquals(Json.Saved(false, "web"), Json.progressSaved(answer("progress-refused")))
        assertEquals(EbookProgress(0.47, "", "web", time), Json.ebookProgress(answer("progress")))
    }

    @Test
    fun readsARequestToBeAddedAndWhatBecameOfIt() {
        assertEquals(
            Pairing("https://gotome.example.org", "0192f0a0-0000-7000-8000-000000000009", "secret-drawn-at-random", "code-drawn-at-random"),
            Json.pairing("https://gotome.example.org", answer("pairing")),
        )
        assertEquals(PairingAnswer("pending", "", ""), Json.pairingAnswer(answer("pairing-pending")))
        assertEquals(PairingAnswer("accepted", "steve", "token-drawn-at-random"), Json.pairingAnswer(answer("pairing-accepted")))
    }

    @Test
    fun readsTheErrorMessage() {
        assertEquals("The user name or the device token is wrong.", Json.errorMessage(answer("error-unauthorized")))
    }
}
