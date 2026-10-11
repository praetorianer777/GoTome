package io.github.praetorianer777.gotome.tolino

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
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
            ReaderEvent.Opened("/storage/emulated/0/Books/GOtome/Hobb-Der_Weitseher-1a2b3c4d.epub"),
            ReaderLog.parse("D/HomeActivity\$DeliverableClickListener( 2245): startReader - book: Der Weitseher, path: /storage/emulated/0/Books/GOtome/Hobb-Der_Weitseher-1a2b3c4d.epub"),
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
            "D/EpubView( 1): notifyPageChanged(): page position = 1.5",
            "D/EpubView( 1): notifyPageChanged(): page position = NaN",
            "D/EpubViewerActivity( 1): openDocument: file:///storage/emulated/0/Books/notes.txt",
            "",
        )
        for (line in others) assertNull(line, ReaderLog.parse(line))
    }
}
