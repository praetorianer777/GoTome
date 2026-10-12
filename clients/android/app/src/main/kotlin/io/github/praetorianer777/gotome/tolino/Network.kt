package io.github.praetorianer777.gotome.tolino

import android.content.Context
import android.net.ConnectivityManager

/**
 * The Tolino turns Wi-Fi off when idle and disables every saved network with it;
 * only its own app may enable them again (another app is refused). So the app
 * cannot go online by itself: it syncs whenever the Tolino is online anyway, and
 * asks the person to turn Wi-Fi on otherwise.
 * The call deprecated since Android 10 is the one Android 8.1 has.
 */
@Suppress("DEPRECATION")
object Network {
    fun online(context: Context): Boolean =
        context.getSystemService(ConnectivityManager::class.java).activeNetworkInfo?.isConnected == true
}
