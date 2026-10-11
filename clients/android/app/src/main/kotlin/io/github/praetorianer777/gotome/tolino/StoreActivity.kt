package io.github.praetorianer777.gotome.tolino

import android.app.Activity
import android.content.Context
import android.content.Intent
import android.os.Bundle
import android.view.GestureDetector
import android.view.KeyEvent
import android.view.MotionEvent
import android.view.View
import android.view.inputmethod.EditorInfo
import android.view.inputmethod.InputMethodManager
import android.widget.Button
import android.widget.EditText
import android.widget.GridLayout
import android.widget.ImageView
import android.widget.TextView
import java.text.DateFormat
import java.util.Date
import kotlin.math.abs

/**
 * The library as a shop on e-ink: nine books a page, turned with ◀ ▶ or a swipe,
 * never scrolled. Pages come from GOtome as they are turned; only the books on
 * the Tolino are listed without network.
 */
class StoreActivity : Activity() {
    private enum class Mode { NEWEST, TITLE, SEARCH, DEVICE }

    private lateinit var prefs: Prefs
    private lateinit var db: Db
    private lateinit var api: Api
    private lateinit var covers: Covers
    private lateinit var grid: GridLayout
    private lateinit var search: EditText
    private lateinit var pageText: TextView
    private lateinit var status: TextView
    private lateinit var tabs: Map<Mode, Button>

    private var mode = Mode.NEWEST
    private var page = 0
    private var total = 0
    /** The cursor each page of a browsed list starts at, as far as it was turned. */
    private val cursors = mutableListOf<String?>(null)
    /** Search hits and the books on the device, paged here rather than on the server. */
    private var local: List<BookSummary> = emptyList()
    private var shown: List<BookSummary> = emptyList()
    private var request = 0
    private var syncing = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        prefs = Prefs(this)
        if (!prefs.signedIn) {
            startActivity(Intent(this, SetupActivity::class.java))
            finish()
            return
        }
        db = Db(this)
        api = Api(prefs)
        covers = Covers(this)
        setContentView(R.layout.activity_store)
        grid = findViewById(R.id.grid)
        search = findViewById(R.id.search)
        pageText = findViewById(R.id.page)
        status = findViewById(R.id.status)
        tabs = mapOf(
            Mode.NEWEST to findViewById(R.id.newest),
            Mode.TITLE to findViewById(R.id.by_title),
            Mode.DEVICE to findViewById(R.id.on_tolino),
        )

        repeat(PER_PAGE) { layoutInflater.inflate(R.layout.cell_book, grid, true) }
        search.setOnEditorActionListener { _, action, _ ->
            if (action == EditorInfo.IME_ACTION_SEARCH) find() else false
        }
        findViewById<Button>(R.id.find).setOnClickListener { find() }
        findViewById<Button>(R.id.previous).setOnClickListener { turn(-1) }
        findViewById<Button>(R.id.next).setOnClickListener { turn(1) }
        findViewById<Button>(R.id.settings).setOnClickListener { startActivity(Intent(this, SetupActivity::class.java)) }
        findViewById<Button>(R.id.sync).setOnClickListener { syncNow() }
        for ((m, tab) in tabs) tab.setOnClickListener { open(m) }

        val swipe = GestureDetector(this, object : GestureDetector.SimpleOnGestureListener() {
            override fun onFling(e1: MotionEvent?, e2: MotionEvent, vx: Float, vy: Float): Boolean {
                val dx = e2.x - (e1?.x ?: return false)
                if (abs(dx) < 120 || abs(dx) < abs(e2.y - e1.y)) return false
                turn(if (dx < 0) 1 else -1)
                return true
            }
        })
        grid.setOnTouchListener { _, event -> swipe.onTouchEvent(event); true }
    }

    override fun onResume() {
        super.onResume()
        if (!prefs.signedIn) return
        ReaderWatcher.start(this)
        SyncJob.schedule(this)
        runCatching { LauncherBook.ensure(this) }.onFailure { prefs.lastError = it.message }
        load()
        if (Network.online(this) && System.currentTimeMillis() - prefs.lastSync > RESYNC) syncNow()
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean = when (keyCode) {
        KeyEvent.KEYCODE_PAGE_DOWN, KeyEvent.KEYCODE_VOLUME_DOWN -> { turn(1); true }
        KeyEvent.KEYCODE_PAGE_UP, KeyEvent.KEYCODE_VOLUME_UP -> { turn(-1); true }
        else -> super.onKeyDown(keyCode, event)
    }

    private fun open(to: Mode) {
        mode = to
        page = 0
        total = 0
        cursors.clear()
        cursors += null
        load()
    }

    private fun find(): Boolean {
        getSystemService(InputMethodManager::class.java).hideSoftInputFromWindow(search.windowToken, 0)
        val words = search.text.toString().trim()
        if (words.isEmpty()) {
            open(Mode.NEWEST)
            return true
        }
        mode = Mode.SEARCH
        page = 0
        local = emptyList()
        fetch {
            Json.searchHits(api.get("/books/search" + Api.query("q" to words, "limit" to "50")))
                .filter { hit -> Storage.READABLE.any { it in hit.formats } }
        }
        return true
    }

    private fun turn(by: Int) {
        val to = page + by
        if (to < 0 || to >= pages()) return
        page = to
        load()
    }

    private fun pages(): Int = when (mode) {
        Mode.NEWEST, Mode.TITLE -> (total + PER_PAGE - 1) / PER_PAGE
        Mode.SEARCH, Mode.DEVICE -> (local.size + PER_PAGE - 1) / PER_PAGE
    }.coerceAtLeast(1)

    /** Shows the current page: from the device, from the search hits, or asked of GOtome. */
    private fun load() {
        db.noteRemoved()
        when (mode) {
            Mode.DEVICE -> {
                local = db.onDevice()
                show(local.drop(page * PER_PAGE).take(PER_PAGE))
            }
            Mode.SEARCH -> show(local.drop(page * PER_PAGE).take(PER_PAGE))
            Mode.NEWEST, Mode.TITLE -> browse()
        }
    }

    private fun browse() {
        val (sort, order) = if (mode == Mode.NEWEST) "added" to "desc" else "title" to "asc"
        val at = page
        val cursor = cursors.getOrNull(at)
        fetch {
            if (total == 0) total = Json.count(api.get("/books/count" + Api.query("filter" to READABLE_FILTER)))
            Json.bookList(api.get("/books" + Api.query(
                "sort" to sort, "order" to order, "limit" to PER_PAGE.toString(),
                "filter" to READABLE_FILTER, "cursor" to cursor,
            )))
        }
    }

    /**
     * Runs a request off the main thread and shows its books; a page of books
     * also brings the cursor of the next. Only the latest request is shown.
     */
    private fun <T> fetch(work: () -> T) {
        if (!Network.online(this)) {
            show(emptyList())
            status.text = getString(R.string.offline_store)
            return
        }
        val mine = ++request
        status.text = getString(R.string.loading)
        Thread {
            val result = runCatching(work)
            runOnUiThread {
                if (isFinishing || mine != request) return@runOnUiThread
                result.onFailure {
                    show(emptyList())
                    status.text = getString(R.string.load_failed, it.message ?: "")
                }
                result.onSuccess { value ->
                    when (value) {
                        is Pair<*, *> -> {
                            @Suppress("UNCHECKED_CAST")
                            val books = value.first as List<BookSummary>
                            val next = value.second as String?
                            if (cursors.size == page + 1 && next != null) cursors += next
                            show(books)
                        }
                        is List<*> -> {
                            @Suppress("UNCHECKED_CAST")
                            local = value as List<BookSummary>
                            show(local.take(PER_PAGE))
                        }
                    }
                }
            }
        }.start()
    }

    private fun show(books: List<BookSummary>) {
        shown = books
        val onDevice = db.onDeviceIds()
        for (i in 0 until PER_PAGE) {
            val cell = grid.getChildAt(i)
            val book = books.getOrNull(i)
            cell.visibility = if (book == null) View.INVISIBLE else View.VISIBLE
            if (book == null) continue
            cell.findViewById<TextView>(R.id.title).text = book.title
            cell.findViewById<TextView>(R.id.author).text = book.authors.joinToString(", ")
            cell.findViewById<TextView>(R.id.mark).visibility = if (book.id in onDevice) View.VISIBLE else View.GONE
            covers.show(book.id, cell.findViewById<ImageView>(R.id.cover))
            cell.setOnClickListener { startActivity(BookActivity.intent(this, book)) }
        }
        pageText.text = when {
            mode == Mode.SEARCH && local.isEmpty() -> getString(R.string.no_hits)
            mode == Mode.DEVICE && local.isEmpty() -> getString(R.string.none_on_device)
            else -> getString(R.string.page_of, page + 1, pages())
        }
        for ((m, tab) in tabs) tab.isSelected = m == mode
        if (!syncing) status.text = statusLine()
    }

    private fun statusLine(): String {
        prefs.lastError?.let { return getString(R.string.sync_failed, it) }
        if (prefs.lastSync == 0L) return getString(R.string.never_synced)
        val time = DateFormat.getTimeInstance(DateFormat.SHORT).format(Date(prefs.lastSync))
        return getString(R.string.synced_at, time)
    }

    private fun syncNow() {
        if (syncing) return
        if (!Network.online(this)) {
            status.text = getString(R.string.offline)
            return
        }
        syncing = true
        status.text = getString(R.string.syncing)
        Thread {
            Sync(this).run { step -> runOnUiThread { if (!isFinishing) status.text = step } }
            runOnUiThread {
                syncing = false
                if (!isFinishing) show(shown)
            }
        }.start()
    }

    companion object {
        private const val PER_PAGE = 9
        private const val RESYNC = 15 * 60 * 1000L
        private const val READABLE_FILTER = """{"field":"format","op":"in","values":["epub","pdf"]}"""

        /** Over the reader app, from the watcher, when the "GOtome" book was opened. */
        fun bringUp(context: Context) {
            context.startActivity(
                Intent(context, StoreActivity::class.java).addFlags(
                    Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP,
                ),
            )
        }
    }
}
