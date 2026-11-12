package io.github.praetorianer777.gotome.tolino

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class ReaderLogTest {
    private val recorded = javaClass.getResource("/tolino-16.2.1.log")!!.readText().lines()
    private val book = "/storage/emulated/0/Books/GoTome-Test.epub"

    @Test
    fun readsTheLinesRecordedOnAShine4() {
        val events = recorded.mapNotNull(ReaderLog::parse)
        assertEquals(ReaderEvent.Opened(book), events.first())
        assertEquals(10, events.count { it is ReaderEvent.Page })
        assertEquals(
            listOf(ReaderEvent.Closed("OEBPS/c1.xhtml#point(/1/2/46/1:0)"), ReaderEvent.Closed("OEBPS/c1.xhtml#point(/1/2/46/1:0)")),
            events.filterIsInstance<ReaderEvent.Closed>(),
        )
    }

    @Test
    fun readsTheBriefFormatTheAppAsksFor() {
        assertEquals(
            ReaderEvent.Page(0.2509080748812517),
            ReaderLog.parse("D/EpubView( 2245): notifyPageChanged(): page position = 0.2509080748812517"),
        )
        assertEquals(
            ReaderEvent.Opened("/storage/emulated/0/Books/GOtome/Hoff-Der_Leuchtfeuerwart-1a2b3c4d.epub"),
            ReaderLog.parse("D/HomeActivity\$DeliverableClickListener( 2245): startReader - book: Der Leuchtfeuerwart, path: /storage/emulated/0/Books/GOtome/Hoff-Der_Leuchtfeuerwart-1a2b3c4d.epub"),
        )
    }

    @Test
    fun aPositionIsInAdobePagesNotAShare() {
        assertEquals(
            ReaderEvent.Page(10.293189368770765),
            ReaderLog.parse("D/EpubView( 6034): notifyPageChanged(): page position = 10.293189368770765"),
        )
    }

    @Test
    fun aTitleWithAPathInItDoesNotConfuseThePath() {
        assertEquals(
            ReaderEvent.Opened(book),
            ReaderLog.parse("D/HomeActivity\$DeliverableClickListener( 1): startReader - book: A, path: B, path: $book"),
        )
    }

    @Test
    fun anSdcardPathIsTheSameFile() {
        assertEquals(ReaderEvent.Opened(book), ReaderLog.parse("D/EpubViewerActivity( 1): openDocument: file:///sdcard/Books/GoTome-Test.epub"))
    }

    @Test
    fun leavesEverythingElseAlone() {
        val others = listOf(
            "I/ShopWebViewActivity\$ShopWebViewClient( 1): cookie uuid=00000000; gcor=\"x\"",
            "D/LoginActivity( 1): LoginWebViewClient::onPageStarted url=epublishing://login?code=abc",
            "D/EpubViewerActivity( 1): openDocument: fontSettings:  ->fontType: publisher",
            "D/EpubView( 1): notifyPageChanged(): page position = -0.5",
            "D/EpubView( 1): notifyPageChanged(): page position = NaN",
            "D/EpubViewerActivity( 1): openDocument: file:///storage/emulated/0/Books/notes.txt",
            "",
        )
        for (line in others) assertNull(line, ReaderLog.parse(line))
    }

    @Test
    fun theViewersFirstScreenIsShown() {
        assertEquals(
            ReaderEvent.Shown,
            ReaderLog.parse("D/EpubViewerActivity( 5258): openDocument: screen begin: OEBPS/c1.xhtml#point(/1/2/1/1:0)"),
        )
        assertTrue(recorded.mapNotNull(ReaderLog::parse).any { it is ReaderEvent.Shown })
        assertNull(ReaderLog.parse("D/EpubViewerActivity( 5258): openDocument: screen end: OEBPS/c1.xhtml#point(/1/2/3/1:70)"))
    }

    @Test
    fun theAppsOwnProbeIsNoEvent() {
        assertNull(ReaderLog.parse("D/${ReaderLog.PROBE}(20575): alive"))
    }
}

class LivenessTest {
    private var now = 1_000L
    private val liveness = Liveness({ now }, silence = 180_000)

    @Test
    fun staysAliveWhileLinesComeIn() {
        repeat(10) {
            now += 60_000
            liveness.heard()
            assertFalse(liveness.stalled())
        }
    }

    @Test
    fun isStalledOnlyPastTheSilence() {
        now += 180_000
        assertFalse(liveness.stalled())
        now += 1
        assertTrue(liveness.stalled())
    }

    @Test
    fun aRestartGivesTheNewReaderTimeBeforeTheNextOne() {
        now += 500_000
        assertTrue(liveness.stalled())
        liveness.restarted()
        assertFalse(liveness.stalled())
        now += 181_000
        assertTrue(liveness.stalled())
    }
}

class StoreRaiseTest {
    @Test
    fun onlyOneOfTheViewerAndTheTimerTakesAnOpening() {
        val raise = StoreRaise()
        assertFalse(raise.take())
        raise.arm()
        assertTrue(raise.take())
        assertFalse(raise.take())
    }
}
