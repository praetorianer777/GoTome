package io.github.praetorianer777.gotome.tolino

/**
 * One GOtome can be reached at several addresses, such as one at home and one
 * on the road. The app keeps them in order, tries them with a short timeout and
 * remembers which worked. The Tolino's Wi-Fi name is not readable on Android 8.1
 * without the location permission, so "per network" is the last address that
 * worked: at home that is the home one, and on the road the first to fail over.
 */
object Addresses {
    /** How long an address has to accept a connection before the next is tried. */
    const val PROBE_TIMEOUT_MS = 4_000

    /** An address as kept: http(s) with a host, no trailing slash; null for anything else. */
    fun clean(address: String): String? {
        val a = address.trim().trimEnd('/')
        return a.takeIf { Regex("^https?://[^/\\s?#@]+(/[^\\s?#]*)?$").matches(it) }
    }

    /** The addresses to try: the one entered, then the ones GOtome named, none twice, the one that worked last first. */
    fun order(entered: String, named: List<String>, lastGood: String?): List<String> {
        val all = (listOf(entered) + named).mapNotNull(::clean).distinct()
        val good = lastGood?.let(::clean)
        return if (good != null && good in all) listOf(good) + all.filter { it != good } else all
    }

    /** The first of [addresses] for which [reaches] says GOtome answered; null when none does. */
    fun choose(addresses: List<String>, reaches: (String) -> Boolean): String? = addresses.firstOrNull(reaches)

    fun join(addresses: List<String>): String = addresses.joinToString("\n")

    fun split(stored: String): List<String> = stored.lines().mapNotNull(::clean).distinct()
}
