package io.github.praetorianer777.gotome.tolino

/** What the Tolino's reader app tells its log about the book being read. */
sealed interface ReaderEvent {
    /** A book was opened, from the home screen, the library or the reader itself. */
    data class Opened(val path: String) : ReaderEvent

    /** A page was turned; where the new page begins, in Adobe's pages (see [AdobePages]). */
    data class Page(val position: Double) : ReaderEvent

    /** The book was closed, at a location in Adobe's notation. */
    data class Closed(val location: String) : ReaderEvent
}

/**
 * Reads the lines the reader app (`de.telekom.epub`, firmware 16.2.1) logs while
 * a book is open. They are its debug output, not an interface, so a firmware
 * update may change them. Everything else in the log, which includes the shop's
 * sign-in while the shop is open, is never matched and never kept.
 */
object ReaderLog {
    /** The tags of the lines below, so that logcat hands the app nothing else. */
    val TAGS = listOf(
        "HomeActivity\$DeliverableClickListener",
        "EpubViewerActivity",
        "EpubView",
        "BasicViewerActivity",
    )

    private val startReader = Regex("""startReader - book: .*, path: (/.+\.(?i:epub|pdf))$""")
    private val openDocument = Regex("""openDocument: file://(/.+\.(?i:epub|pdf))$""")
    private val page = Regex("""notifyPageChanged\(\): page position = ([0-9.Ee+-]+)$""")
    private val closed = Regex("""TableDeliverable\.BOOKMARK_POSITION:= (\S+)$""")

    fun parse(line: String): ReaderEvent? {
        val text = line.trimEnd()
        startReader.find(text)?.let { return ReaderEvent.Opened(Storage.canonical(it.groupValues[1])) }
        openDocument.find(text)?.let { return ReaderEvent.Opened(Storage.canonical(it.groupValues[1])) }
        page.find(text)?.let { match ->
            val position = match.groupValues[1].toDoubleOrNull() ?: return null
            return if (position.isFinite() && position >= 0) ReaderEvent.Page(position) else null
        }
        closed.find(text)?.let { return ReaderEvent.Closed(it.groupValues[1]) }
        return null
    }
}

/** What a reader event means for the book being read. */
sealed interface Reading {
    data class Opened(val path: String) : Reading
    data class Moved(val path: String, val position: Double) : Reading
    data class Closed(val path: String) : Reading
}

/** Follows the reader's events to which book is open and where in it the person is. */
class ReadingTracker {
    var current: String? = null
        private set

    fun on(event: ReaderEvent): Reading? = when (event) {
        // The home screen and the viewer both report the same opening.
        is ReaderEvent.Opened -> if (event.path == current) null else {
            current = event.path
            Reading.Opened(event.path)
        }
        is ReaderEvent.Page -> current?.let { Reading.Moved(it, event.position) }
        is ReaderEvent.Closed -> current?.let {
            current = null
            Reading.Closed(it)
        }
    }
}
