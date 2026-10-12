package io.github.praetorianer777.gotome.tolino

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/** Starts following the reader after a restart of the Tolino and after an update of the app. */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (!Prefs(context).signedIn) return
        ReaderWatcher.start(context)
        SyncJob.schedule(context)
    }
}
