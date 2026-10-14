package io.github.praetorianer777.gotome.tolino

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.GestureDescription
import android.content.Context
import android.graphics.Path
import android.graphics.PixelFormat
import android.graphics.Rect
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import android.view.ContextThemeWrapper
import android.view.Gravity
import android.view.LayoutInflater
import android.view.View
import android.view.WindowManager
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityNodeInfo
import android.widget.Button
import android.widget.TextView
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

/**
 * Moves the Tolino reader to a page through its own "Go to page" dialog, which
 * nothing else can do: the reader takes no position from other apps. Offers the
 * jump in a bar over the reader and acts only on "Jump"; its configuration limits
 * it to the reader app. Enabled over adb (make android-install).
 */
class JumpService : AccessibilityService() {
    override fun attachBaseContext(base: Context) {
        super.attachBaseContext(Lang.wrap(base))
    }

    private val main = Handler(Looper.getMainLooper())
    private var bar: View? = null

    override fun onServiceConnected() {
        instance = this
    }

    override fun onUnbind(intent: android.content.Intent?): Boolean {
        instance = null
        main.post { hide() }
        return super.onUnbind(intent)
    }

    override fun onAccessibilityEvent(event: AccessibilityEvent?) = Unit

    override fun onInterrupt() = Unit

    fun offer(page: Int, total: Int, percent: Int) = main.post {
        show(getString(R.string.jump_offer, page, total, percent), getString(R.string.jump_yes)) {
            Thread {
                val done = runCatching { jump(page) }.getOrDefault(false)
                if (!done) main.post { show(getString(R.string.jump_failed, page), null) {} }
            }.start()
        }
    }

    fun withdraw() = main.post { hide() }

    /** A bar at the bottom over the reader; touches outside it still reach the reader. */
    private fun show(text: String, yes: String?, onYes: () -> Unit) {
        hide()
        val view = LayoutInflater.from(ContextThemeWrapper(this, R.style.Ink)).inflate(R.layout.overlay_jump, null)
        view.findViewById<TextView>(R.id.text).text = text
        view.findViewById<Button>(R.id.yes).apply {
            if (yes == null) visibility = View.GONE else this.text = yes
            setOnClickListener { hide(); onYes() }
        }
        view.findViewById<Button>(R.id.no).setOnClickListener { hide() }
        val params = WindowManager.LayoutParams(
            WindowManager.LayoutParams.MATCH_PARENT,
            WindowManager.LayoutParams.WRAP_CONTENT,
            WindowManager.LayoutParams.TYPE_ACCESSIBILITY_OVERLAY,
            WindowManager.LayoutParams.FLAG_NOT_FOCUSABLE or WindowManager.LayoutParams.FLAG_NOT_TOUCH_MODAL,
            PixelFormat.OPAQUE,
        ).apply { gravity = Gravity.BOTTOM }
        getSystemService(WindowManager::class.java).addView(view, params)
        bar = view
        main.postDelayed({ if (bar === view) hide() }, SHOWN_FOR)
    }

    private fun hide() {
        bar?.let { runCatching { getSystemService(WindowManager::class.java).removeView(it) } }
        bar = null
    }

    /** Menu, page number, digits, OK: the steps a person takes, by the reader's view IDs. */
    private fun jump(page: Int): Boolean {
        if (find(OK) == null) {
            if (find(PAGE_NUMBER) == null) {
                tap(find(VIEW)?.let(::centre) ?: return false)
                waitFor(PAGE_NUMBER) ?: return false
            }
            click(find(PAGE_NUMBER) ?: return false)
            waitFor(OK) ?: return false
        }
        for (digit in page.toString()) {
            click(waitFor(KEY + digit) ?: return false)
            Thread.sleep(STEP)
        }
        click(waitFor(OK) ?: return false)
        return landedOn(page)
    }

    /** The reader names its page in the view's description ("pageNr=13,…"). */
    private fun landedOn(page: Int): Boolean {
        repeat(20) {
            Thread.sleep(STEP)
            val description = find(VIEW)?.contentDescription?.toString() ?: return true
            val now = Regex("""pageNr=(\d+)""").find(description)?.groupValues?.get(1)?.toInt() ?: return true
            if (now in page - 1..page + 1) return true
        }
        return false
    }

    private fun find(id: String): AccessibilityNodeInfo? =
        rootInActiveWindow?.findAccessibilityNodeInfosByViewId("$READER:id/$id")?.firstOrNull()

    private fun waitFor(id: String): AccessibilityNodeInfo? {
        repeat(25) {
            find(id)?.let { return it }
            Thread.sleep(STEP)
        }
        return null
    }

    private fun click(node: AccessibilityNodeInfo) {
        if (!node.performAction(AccessibilityNodeInfo.ACTION_CLICK)) tap(centre(node))
    }

    private fun centre(node: AccessibilityNodeInfo): Pair<Float, Float> =
        Rect().also(node::getBoundsInScreen).let { it.exactCenterX() to it.exactCenterY() }

    private fun tap(at: Pair<Float, Float>) {
        val path = Path().apply { moveTo(at.first, at.second) }
        val gesture = GestureDescription.Builder().addStroke(GestureDescription.StrokeDescription(path, 0, 60)).build()
        val done = CountDownLatch(1)
        dispatchGesture(gesture, object : GestureResultCallback() {
            override fun onCompleted(gestureDescription: GestureDescription?) = done.countDown()
            override fun onCancelled(gestureDescription: GestureDescription?) = done.countDown()
        }, main)
        done.await(2, TimeUnit.SECONDS)
    }

    companion object {
        private const val READER = "de.telekom.epub"
        private const val VIEW = "epub_viewer_activity_epub_view"
        private const val PAGE_NUMBER = "viewer_page_navigation_menu_seekbar_page_position"
        private const val KEY = "application_content_paging_keyboard_button"
        private const val OK = "application_content_paging_keyboard_buttonok"
        private const val STEP = 200L
        private const val SHOWN_FOR = 60_000L

        @Volatile
        var instance: JumpService? = null
            private set

        fun enabled(context: Context): Boolean =
            Settings.Secure.getString(context.contentResolver, Settings.Secure.ENABLED_ACCESSIBILITY_SERVICES)
                ?.contains("${context.packageName}/") == true
    }
}
