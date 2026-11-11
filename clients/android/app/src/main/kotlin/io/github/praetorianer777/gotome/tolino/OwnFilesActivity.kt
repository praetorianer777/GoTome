package io.github.praetorianer777.gotome.tolino

import android.app.Activity
import android.content.Context
import android.os.Bundle
import android.view.Gravity
import android.view.ViewGroup
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import java.io.File

/**
 * "Not in GOtome": the DRM-free EPUBs and PDFs on the Tolino that GOtome has no
 * book for, each with a button to upload it. Where the device may not upload,
 * the page says why instead.
 */
class OwnFilesActivity : Activity() {
    override fun attachBaseContext(base: Context) {
        super.attachBaseContext(Lang.wrap(base))
    }

    private lateinit var prefs: Prefs
    private lateinit var own: OwnDb
    private lateinit var db: Db
    private lateinit var list: LinearLayout
    private lateinit var note: TextView
    private lateinit var status: TextView
    private lateinit var pageText: TextView
    private var files: List<OwnFile> = emptyList()
    private var state: UploadState? = null
    private var page = 0
    private var busy = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        prefs = Prefs(this)
        if (!prefs.signedIn) {
            finish()
            return
        }
        own = OwnDb.of(this)
        db = Db.of(this)
        val pad = (12 * resources.displayMetrics.density).toInt()
        val root = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(pad, pad, pad, pad)
        }
        root.addView(TextView(this).apply {
            text = getString(R.string.own_title)
            textSize = 24f
        })
        note = TextView(this).apply { textSize = 17f; setPadding(0, pad / 2, 0, pad / 2) }
        root.addView(note)
        list = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL }
        root.addView(list, LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, 0, 1f))

        val bar = LinearLayout(this).apply { gravity = Gravity.CENTER_VERTICAL }
        bar.addView(button(R.string.back) { finish() })
        bar.addView(arrow("◀", R.string.previous) { turn(-1) })
        pageText = TextView(this).apply { gravity = Gravity.CENTER; textSize = 17f }
        bar.addView(pageText, LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f))
        bar.addView(arrow("▶", R.string.next) { turn(1) })
        root.addView(bar)
        status = TextView(this).apply { textSize = 15f; setPadding(0, pad / 2, 0, 0) }
        root.addView(status)
        setContentView(root)
    }

    override fun onResume() {
        super.onResume()
        if (!prefs.signedIn) return
        files = own.unmatched().filter { File(it.path).exists() }
        show()
        if (Network.online(this)) {
            Thread {
                val result = runCatching { OwnBooks(this, db, Api(prefs)).uploadState() }
                runOnUiThread {
                    if (isFinishing) return@runOnUiThread
                    result.onSuccess { state = it }
                    result.onFailure { status.text = getString(R.string.load_failed, it.message ?: "") }
                    show()
                }
            }.start()
        } else {
            status.text = getString(R.string.offline)
        }
    }

    override fun onDestroy() {
        if (::own.isInitialized) own.close()
        if (::db.isInitialized) db.close()
        super.onDestroy()
    }

    private fun button(label: Int, click: () -> Unit) = Button(this).apply {
        text = getString(label)
        setOnClickListener { click() }
    }

    private fun arrow(symbol: String, description: Int, click: () -> Unit) = Button(this).apply {
        text = symbol
        contentDescription = getString(description)
        setOnClickListener { click() }
    }

    private fun turn(by: Int) {
        val to = page + by
        if (to < 0 || to >= pages()) return
        page = to
        show()
    }

    private fun pages() = ((files.size + PER_PAGE - 1) / PER_PAGE).coerceAtLeast(1)

    private fun show() {
        page = page.coerceAtMost(pages() - 1)
        val s = state
        note.text = when {
            s == null -> ""
            s.allowed -> getString(R.string.own_goes_to, s.library.orEmpty())
            else -> getString(
                when (s.reason) {
                    "token" -> R.string.own_no_token
                    "role" -> R.string.own_no_role
                    "library" -> R.string.own_no_library
                    "quota" -> R.string.own_quota
                    else -> R.string.own_no_upload
                },
            )
        }
        list.removeAllViews()
        for (file in files.drop(page * PER_PAGE).take(PER_PAGE)) {
            val row = LinearLayout(this).apply {
                gravity = Gravity.CENTER_VERTICAL
                setPadding(0, 6, 0, 6)
            }
            row.addView(TextView(this).apply {
                text = File(file.path).name + "\n" + android.text.format.Formatter.formatShortFileSize(context, file.size)
                textSize = 17f
            }, LinearLayout.LayoutParams(0, ViewGroup.LayoutParams.WRAP_CONTENT, 1f))
            if (s?.allowed == true) {
                row.addView(Button(this).apply {
                    text = getString(R.string.own_upload)
                    isEnabled = !busy
                    setOnClickListener { upload(file) }
                })
            }
            list.addView(row)
        }
        pageText.text = if (files.isEmpty()) getString(R.string.own_none) else getString(R.string.page_of, page + 1, pages())
    }

    private fun upload(file: OwnFile) {
        if (busy) return
        busy = true
        status.text = getString(R.string.own_uploading, File(file.path).name)
        show()
        Thread {
            val result = runCatching { OwnBooks(this, db, Api(prefs)).upload(file.path) }
            runOnUiThread {
                busy = false
                if (isFinishing) return@runOnUiThread
                result.onSuccess {
                    files = files.filter { it.path != file.path }
                    status.text = getString(R.string.own_uploaded, File(file.path).name)
                }
                result.onFailure { status.text = getString(R.string.own_upload_failed, it.message ?: "") }
                show()
            }
        }.start()
    }

    companion object {
        private const val PER_PAGE = 6
    }
}
