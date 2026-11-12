package io.github.praetorianer777.gotome.tolino

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ReadingTrackerTest {
    private val book = "/storage/emulated/0/Books/GoTome-Test.epub"

    @Test
    fun followsTheRecordedSessionsToTheirLastPosition() {
        val tracker = ReadingTracker()
        val readings = javaClass.getResource("/tolino-16.2.1.log")!!.readText().lines()
            .mapNotNull(ReaderLog::parse)
            .mapNotNull(tracker::on)
        assertEquals(listOf(Reading.Opened(book), Reading.Closed(book)), readings.filter { it !is Reading.Moved }.take(2))
        assertEquals(2, readings.count { it is Reading.Closed })
        assertEquals(Reading.Moved(book, 0.2509080748812517), readings.filterIsInstance<Reading.Moved>().last())
        assertNull(tracker.current)
    }

    @Test
    fun theHomeScreenAndTheViewerOpenTheBookOnce() {
        val tracker = ReadingTracker()
        assertEquals(Reading.Opened(book), tracker.on(ReaderEvent.Opened(book)))
        assertNull(tracker.on(ReaderEvent.Opened(book)))
    }

    @Test
    fun aPageWithoutAnOpenBookIsNobodys() {
        val tracker = ReadingTracker()
        assertNull(tracker.on(ReaderEvent.Page(0.5)))
        assertNull(tracker.on(ReaderEvent.Closed("x")))
    }

    @Test
    fun openingAnotherBookMovesThePagesThere() {
        val tracker = ReadingTracker()
        tracker.on(ReaderEvent.Opened(book))
        tracker.on(ReaderEvent.Opened("/other.epub"))
        assertEquals(Reading.Moved("/other.epub", 0.1), tracker.on(ReaderEvent.Page(0.1)))
    }
}
