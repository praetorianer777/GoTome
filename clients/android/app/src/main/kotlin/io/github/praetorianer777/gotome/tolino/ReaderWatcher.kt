package io.github.praetorianer777.gotome.tolino

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.util.Log

/**
 * Follows the reader app through the log: keeps the position in every book the
 * app delivered, sends it once the book is closed, and brings the store up when
 * the "GOtome" book is opened. Runs in the foreground so Android keeps it alive.
 */
class ReaderWatcher : Service() {
    private val main = Handler(Looper.getMainLooper())
    private val tracker = ReadingTracker()
    private lateinit var prefs: Prefs
    private lateinit var db: Db
    @Volatile private var reader: Thread? = null
    @Volatile private var logcat: Process? = null
    private var sentAt = 0L

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
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (reader?.isAlive != true) {
            reader = Thread(::follow, "reader-log").apply { isDaemon = true; start() }
        }
        return START_STICKY
    }

    override fun onDestroy() {
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
            is Reading.Moved -> db.deliveredAt(reading.path)?.let { book ->
                db.saveProgress(book, reading.fraction, now)
                // Long sessions reach GOtome now and then, not only at the end.
                if (now - sentAt > SEND_EVERY) send(now)
            }
            is Reading.Closed -> if (db.deliveredAt(reading.path) != null) send(now)
            else -> Unit
        }
    }

    private fun send(now: Long) {
        sentAt = now
        SyncJob.now(this)
    }

    companion object {
        private const val TAG = "GOtomeWatcher"
        private const val CHANNEL = "watcher"
        private const val SEND_EVERY = 10 * 60 * 1000L

        fun start(context: Context) {
            context.startForegroundService(Intent(context, ReaderWatcher::class.java))
        }

        fun canReadLog(context: Context): Boolean =
            context.checkSelfPermission(android.Manifest.permission.READ_LOGS) == PackageManager.PERMISSION_GRANTED
    }
}
