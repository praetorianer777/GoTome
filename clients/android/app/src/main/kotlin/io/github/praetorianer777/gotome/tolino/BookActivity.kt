package io.github.praetorianer777.gotome.tolino

import android.app.Activity
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.view.View
import android.widget.Button
import android.widget.ImageView
import android.widget.TextView
import java.text.DateFormat
import java.text.SimpleDateFormat
import java.util.Locale
import kotlin.math.roundToInt

/** One book: what it is, where GOtome says the person is in it, and the button that puts it on the Tolino. */
class BookActivity : Activity() {
    private lateinit var db: Db
    private lateinit var sync: Sync
    private lateinit var bookId: String
    private lateinit var action: Button
    private lateinit var state: TextView
    private lateinit var progress: TextView
    private var confirming = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        db = Db(this)
        sync = Sync(this)
        bookId = intent.getStringExtra(BOOK) ?: return finish()
        val book = BookSummary(
            id = bookId,
            title = intent.getStringExtra(TITLE).orEmpty(),
            authors = intent.getStringArrayExtra(AUTHORS)?.toList().orEmpty(),
            series = intent.getStringExtra(SERIES).orEmpty(),
            seriesIndex = if (intent.hasExtra(SERIES_INDEX)) intent.getDoubleExtra(SERIES_INDEX, 0.0) else null,
            formats = emptyList(),
            addedAt = "",
        )
        setContentView(R.layout.activity_book)
        action = findViewById(R.id.action)
        state = findViewById(R.id.state)
        progress = findViewById(R.id.progress)

        findViewById<TextView>(R.id.title).text = book.title
        findViewById<TextView>(R.id.author).text = book.authors.joinToString(", ")
        findViewById<TextView>(R.id.series).apply {
            text = when {
                book.series.isEmpty() -> ""
                book.seriesIndex == null -> book.series
                else -> getString(R.string.series_part, book.series, number(book.seriesIndex))
            }
            visibility = if (book.series.isEmpty()) View.GONE else View.VISIBLE
        }
        Covers(this).show(bookId, findViewById<ImageView>(R.id.cover), "large")
        findViewById<Button>(R.id.back).setOnClickListener { finish() }
        action.setOnClickListener { if (db.delivered(bookId) == null) deliver() else remove() }
        showState()
        loadDetails()
    }

    private fun showState() {
        db.noteRemoved()
        val onDevice = db.delivered(bookId) != null
        state.text = getString(if (onDevice) R.string.on_tolino else R.string.not_on_tolino)
        action.text = getString(if (onDevice) R.string.remove else R.string.download)
        confirming = false
    }

    /** Asks once on the button itself; a dialog would cost two more refreshes. */
    private fun remove() {
        if (!confirming) {
            confirming = true
            action.text = getString(R.string.remove_confirm)
            return
        }
        action.isEnabled = false
        Thread {
            sync.remove(bookId)
            runOnUiThread {
                if (isFinishing) return@runOnUiThread
                action.isEnabled = true
                showState()
                state.text = getString(R.string.removed)
            }
        }.start()
    }

    /** The description and GOtome's position need the network; the page shows without them. */
    private fun loadDetails() {
        Thread {
            val detail = runCatching { Json.bookDetail(Api(Prefs(this)).get("/books/$bookId")) }.getOrNull()
            val where = runCatching { sync.ebookProgress(bookId) }.getOrNull()
            runOnUiThread {
                if (isFinishing) return@runOnUiThread
                detail?.description?.takeIf { it.isNotBlank() }?.let {
                    findViewById<TextView>(R.id.description).text = android.text.Html.fromHtml(it, 0)
                }
                if (where != null && where.fraction > 0) {
                    progress.visibility = View.VISIBLE
                    progress.text = getString(R.string.gotome_position, (where.fraction * 100).roundToInt(), when {
                        where.clientId == Prefs(this).clientId -> getString(R.string.on_this_tolino)
                        else -> getString(R.string.elsewhere, date(where.updatedAt))
                    })
                }
            }
        }.start()
    }

    private fun deliver() {
        action.isEnabled = false
        state.text = getString(R.string.downloading)
        Thread {
            val result = runCatching {
                if (!Network.online(this)) throw ApiException(0, getString(R.string.offline))
                sync.deliver(bookId)
            }
            runOnUiThread {
                if (isFinishing) return@runOnUiThread
                action.isEnabled = true
                result.onSuccess { showState(); state.text = getString(R.string.delivered) }
                result.onFailure { state.text = getString(R.string.download_failed, it.message ?: "") }
            }
        }.start()
    }

    private fun number(n: Double) = if (n == n.toLong().toDouble()) n.toLong().toString() else n.toString()

    private fun date(iso: String): String = runCatching {
        val parsed = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss", Locale.ROOT).parse(iso.take(19))!!
        DateFormat.getDateInstance(DateFormat.MEDIUM).format(parsed)
    }.getOrDefault(iso.take(10))

    companion object {
        private const val BOOK = "book"
        private const val TITLE = "title"
        private const val AUTHORS = "authors"
        private const val SERIES = "series"
        private const val SERIES_INDEX = "seriesIndex"

        fun intent(context: Context, book: BookSummary) = Intent(context, BookActivity::class.java)
            .putExtra(BOOK, book.id)
            .putExtra(TITLE, book.title)
            .putExtra(AUTHORS, book.authors.toTypedArray())
            .putExtra(SERIES, book.series)
            .apply { book.seriesIndex?.let { putExtra(SERIES_INDEX, it) } }
    }
}
