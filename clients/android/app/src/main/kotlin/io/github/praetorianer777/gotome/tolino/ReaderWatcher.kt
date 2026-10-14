package io.github.praetorianer777.gotome.tolino

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.ConnectivityManager
import android.net.Network as AndroidNetwork
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.util.Log
import java.io.File
import kotlin.math.roundToInt

/**
 * Follows the reader app through the log: keeps the position in every book the
 * app delivered, sends it once the book is closed, and brings the store up when
 * the "GOtome" book is opened. Runs in the foreground so Android keeps it alive.
 */
class ReaderWatcher : Service() {
    override fun attachBaseContext(base: Context) {
        super.attachBaseContext(Lang.wrap(base))
    }

    private val main = Handler(Looper.getMainLooper())
    private val tracker = ReadingTracker()
    private lateinit var prefs: Prefs
    private lateinit var db: Db
    @Volatile private var reader: Thread? = null
    @Volatile private var logcat: Process? = null
    private var sentAt = 0L
    private val pages = HashMap<String, Int>()
    /** The book just opened, until its first position decides whether to offer a jump. */
    private var opened: String? = null

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        prefs = Prefs(this)
        db = Db(this)
        val channel = NotificationChannel(CHANNEL, getString(R.string.watcher_channel), NotificationManager.IMPORTANCE_MIN)
        getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
        startForeground(1, Notification.Builder(this, CHANNEL)
            .setSmallIcon(R.drawable.ic_book)
            .setContentTitle(getString(R.string.watcher_title))
            .build())
        connectivity.registerDefaultNetworkCallback(online)
    }

    /** Syncs whenever the Tolino goes online, at most every few minutes. */
    private val online = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: AndroidNetwork) {
            val now = System.currentTimeMillis()
            if (now - onlineAt < SYNC_ONLINE_EVERY) return
            onlineAt = now
            SyncJob.now(this@ReaderWatcher)
        }
    }
    @Volatile private var onlineAt = 0L
    private val connectivity by lazy { getSystemService(ConnectivityManager::class.java) }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (reader?.isAlive != true) {
            reader = Thread(::follow, "reader-log").apply { isDaemon = true; start() }
        }
        return START_STICKY
    }

    override fun onDestroy() {
        connectivity.unregisterNetworkCallback(online)
        reader?.interrupt()
        logcat?.destroy()
        super.onDestroy()
    }

    private fun follow() {
        while (!Thread.currentThread().isInterrupted) {
            try {
                // Only the reader's tags reach the app; "-T 1" skips what was logged before.
                val command = listOf("logcat", "-v", "brief", "-T", "1") + ReaderLog.TAGS.map { "$it:D" } + "*:S"
                val process = ProcessBuilder(command).redirectErrorStream(true).start()
                logcat = process
                process.inputStream.bufferedReader().useLines { lines ->
                    lines.forEach { line -> ReaderLog.parse(line)?.let(::handle) }
                }
            } catch (e: Exception) {
                Log.w(TAG, "logcat ended: ${e.javaClass.simpleName}")
            }
            try {
                Thread.sleep(5_000)
            } catch (e: InterruptedException) {
                return
            }
        }
    }

    private fun handle(event: ReaderEvent) {
        val now = System.currentTimeMillis()
        prefs.readerSeenAt = now
        if (event is ReaderEvent.Opened && event.path == Storage.LAUNCHER) {
            // The viewer starts right after the line; the store has to come after it.
            main.removeCallbacksAndMessages(null)
            main.postDelayed({ StoreActivity.bringUp(this) }, 800)
        }
        when (val reading = tracker.on(event)) {
            is Reading.Opened -> opened = reading.path
            is Reading.Moved -> db.deliveredAt(reading.path)?.let { book ->
                val fraction = fraction(reading) ?: return
                if (opened == reading.path) {
                    opened = null
                    // Off the log's thread: asking GOtome first may take a moment.
                    Thread { offerJump(book, reading) }.start()
                }
                db.saveProgress(book, fraction, now)
                // Long sessions reach GOtome now and then, not only at the end.
                if (now - sentAt > SEND_EVERY) send(now)
            }
            is Reading.Closed -> {
                JumpService.instance?.withdraw()
                if (db.deliveredAt(reading.path) != null) send(now)
            }
            null -> Unit
        }
    }

    /** Asks GOtome for the book's position when online, else goes by the last sync. */
    private fun offerJump(book: Delivered, reading: Reading.Moved) {
        val service = JumpService.instance ?: return
        val total = pages[reading.path] ?: return
        if (prefs.signedIn && Network.online(this)) runCatching { Sync(this).fetchPosition(book.bookId) }
        val remote = db.remote(book.bookId) ?: return
        val page = Jump.offer(reading.position, total, remote, prefs.clientId) ?: return
        if (!db.firstOffer("${book.bookId}@${remote.updatedAt}")) return
        service.offer(page, total, (remote.fraction * 100).roundToInt())
    }

    /** Only EPUBs: the reader's positions in a PDF are not in Adobe's EPUB pages. */
    private fun fraction(reading: Reading.Moved): Double? {
        if (!reading.path.endsWith(".epub", ignoreCase = true)) return null
        val total = pages.getOrPut(reading.path) { runCatching { AdobePages.count(File(reading.path)) }.getOrDefault(0) }
        return if (total > 0) (reading.position / total).coerceIn(0.0, 1.0) else null
    }

    private fun send(now: Long) {
        sentAt = now
        SyncJob.now(this)
    }

    companion object {
        private const val TAG = "GOtomeWatcher"
        private const val CHANNEL = "watcher"
        private const val SEND_EVERY = 10 * 60 * 1000L
        private const val SYNC_ONLINE_EVERY = 5 * 60 * 1000L

        fun start(context: Context) {
            context.startForegroundService(Intent(context, ReaderWatcher::class.java))
        }

        fun canReadLog(context: Context): Boolean =
            context.checkSelfPermission(android.Manifest.permission.READ_LOGS) == PackageManager.PERMISSION_GRANTED
    }
}
