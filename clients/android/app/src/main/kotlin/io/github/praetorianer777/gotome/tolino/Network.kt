package io.github.praetorianer777.gotome.tolino

import android.content.Context
import android.net.ConnectivityManager
import android.net.wifi.WifiManager

/**
 * The Tolino turns Wi-Fi off when idle; a tap in the store turns it on again.
 * The calls deprecated since Android 10 are the ones Android 8.1 has.
 */
@Suppress("DEPRECATION")
object Network {
    fun online(context: Context): Boolean =
        context.getSystemService(ConnectivityManager::class.java).activeNetworkInfo?.isConnected == true

    /** Turns Wi-Fi on when it is off and waits up to [seconds] for a connection. */
    fun ensure(context: Context, seconds: Int = 25): Boolean {
        if (online(context)) return true
        val wifi = context.applicationContext.getSystemService(WifiManager::class.java)
        if (!wifi.isWifiEnabled) wifi.isWifiEnabled = true
        repeat(seconds) {
            Thread.sleep(1_000)
            if (online(context)) return true
        }
        return false
    }
}
