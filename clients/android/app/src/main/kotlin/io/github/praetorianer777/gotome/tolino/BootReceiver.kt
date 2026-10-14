package io.github.praetorianer777.gotome.tolino

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** Starts following the reader after a restart of the Tolino and after an update of the app. */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        // Signed in or not: the watcher is what opens the app from the GOtome
        // book, the only way into it from the Tolino's home (#245).
        ReaderWatcher.start(context)
        if (Prefs(context).signedIn) SyncJob.schedule(context)
    }
}
