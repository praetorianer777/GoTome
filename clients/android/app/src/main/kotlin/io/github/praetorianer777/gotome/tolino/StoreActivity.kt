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
 * never scrolled. Everything comes from the catalogue on the device.
 */
class StoreActivity : Activity() {
    private lateinit var prefs: Prefs
    private lateinit var db: Db
    private lateinit var covers: Covers
    private lateinit var grid: GridLayout
    private lateinit var search: EditText
    private lateinit var pageText: TextView
    private lateinit var status: TextView
    private lateinit var newest: Button
    private lateinit var byTitle: Button
    private lateinit var onTolino: Button
    private var page = 0
    private var words = ""
    private var onDevice = false
    private var order = Db.Order.ADDED

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        prefs = Prefs(this)
        if (!prefs.signedIn) {
            startActivity(Intent(this, SetupActivity::class.java))
            finish()
            return
        }
        db = Db(this)
        covers = Covers(this)
        setContentView(R.layout.activity_store)
        grid = findViewById(R.id.grid)
        search = findViewById(R.id.search)
        pageText = findViewById(R.id.page)
        status = findViewById(R.id.status)
        newest = findViewById(R.id.newest)
        byTitle = findViewById(R.id.by_title)
        onTolino = findViewById(R.id.on_tolino)

        repeat(PER_PAGE) { layoutInflater.inflate(R.layout.cell_book, grid, true) }
        search.setOnEditorActionListener { _, action, _ ->
            if (action == EditorInfo.IME_ACTION_SEARCH) find() else false
        }
        findViewById<Button>(R.id.find).setOnClickListener { find() }
        findViewById<Button>(R.id.previous).setOnClickListener { turn(-1) }
        findViewById<Button>(R.id.next).setOnClickListener { turn(1) }
        findViewById<Button>(R.id.settings).setOnClickListener { startActivity(Intent(this, SetupActivity::class.java)) }
        findViewById<Button>(R.id.sync).setOnClickListener { syncNow() }
        newest.setOnClickListener { order = Db.Order.ADDED; onDevice = false; page = 0; render() }
        byTitle.setOnClickListener { order = Db.Order.TITLE; onDevice = false; page = 0; render() }
        onTolino.setOnClickListener { onDevice = true; page = 0; render() }

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
        if (prefs.catalogueAt == 0L) syncNow() else render()
    }

    override fun onKeyDown(keyCode: Int, event: KeyEvent): Boolean = when (keyCode) {
        KeyEvent.KEYCODE_PAGE_DOWN, KeyEvent.KEYCODE_VOLUME_DOWN -> { turn(1); true }
        KeyEvent.KEYCODE_PAGE_UP, KeyEvent.KEYCODE_VOLUME_UP -> { turn(-1); true }
        else -> super.onKeyDown(keyCode, event)
    }

    private fun find(): Boolean {
        words = search.text.toString()
        page = 0
        getSystemService(InputMethodManager::class.java).hideSoftInputFromWindow(search.windowToken, 0)
        render()
        return true
    }

    private fun turn(by: Int) {
        val pages = pages()
        val to = (page + by).coerceIn(0, pages - 1)
        if (to != page) {
            page = to
            render()
        }
    }

    private fun pages() = ((db.count(words, onDevice) + PER_PAGE - 1) / PER_PAGE).coerceAtLeast(1)

    private fun render() {
        db.noteRemoved()
        val delivered = db.onDeviceIds()
        val books = db.catalogue(words, onDevice, order, page * PER_PAGE, PER_PAGE)
        for (i in 0 until PER_PAGE) {
            val cell = grid.getChildAt(i)
            val book = books.getOrNull(i)
            cell.visibility = if (book == null) View.INVISIBLE else View.VISIBLE
            if (book == null) continue
            cell.findViewById<TextView>(R.id.title).text = book.title
            cell.findViewById<TextView>(R.id.author).text = book.authors.joinToString(", ")
            cell.findViewById<TextView>(R.id.mark).visibility = if (book.id in delivered) View.VISIBLE else View.GONE
            covers.show(book.id, cell.findViewById<ImageView>(R.id.cover))
            cell.setOnClickListener { startActivity(BookActivity.intent(this, book.id)) }
        }
        pageText.text = getString(R.string.page_of, page + 1, pages())
        for ((button, on) in listOf(
            newest to (!onDevice && order == Db.Order.ADDED),
            byTitle to (!onDevice && order == Db.Order.TITLE),
            onTolino to onDevice,
        )) button.isSelected = on
        status.text = statusLine()
    }

    private fun statusLine(): String {
        prefs.lastError?.let { return getString(R.string.sync_failed, it) }
        if (prefs.lastSync == 0L) return getString(R.string.never_synced)
        val time = DateFormat.getTimeInstance(DateFormat.SHORT).format(Date(prefs.lastSync))
        return getString(R.string.synced_at, db.count("", false), time)
    }

    private fun syncNow() {
        status.text = getString(R.string.syncing)
        Thread {
            val online = Network.ensure(this)
            if (online) Sync(this).run(refreshCatalogue = true) else prefs.lastError = getString(R.string.offline)
            runOnUiThread { if (!isFinishing) render() }
        }.start()
    }

    companion object {
        private const val PER_PAGE = 9

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
