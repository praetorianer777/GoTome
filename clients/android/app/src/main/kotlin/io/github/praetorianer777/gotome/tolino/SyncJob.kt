package io.github.praetorianer777.gotome.tolino

import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.ComponentName
import android.content.Context

/**
 * Syncs whenever Android sees a network: once at once when asked, and every hour
 * besides. The Tolino turns Wi-Fi off when idle, so a job waits for it to return.
 */
class SyncJob : JobService() {
    override fun attachBaseContext(base: Context) {
        super.attachBaseContext(Lang.wrap(base))
    }

    override fun onStartJob(params: JobParameters): Boolean {
        Thread({
            Sync(this).run()
            jobFinished(params, false)
        }, "sync").start()
        return true
    }

    override fun onStopJob(params: JobParameters): Boolean = true

    companion object {
        private const val PERIODIC = 1
        private const val NOW = 2

        fun schedule(context: Context) {
            scheduler(context).schedule(
                JobInfo.Builder(PERIODIC, ComponentName(context, SyncJob::class.java))
                    .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                    .setPeriodic(60 * 60 * 1000L)
                    .setPersisted(true)
                    .build(),
            )
        }

        fun now(context: Context) {
            scheduler(context).schedule(
                JobInfo.Builder(NOW, ComponentName(context, SyncJob::class.java))
                    .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                    .build(),
            )
        }

        private fun scheduler(context: Context) = context.getSystemService(JobScheduler::class.java)
    }
}
