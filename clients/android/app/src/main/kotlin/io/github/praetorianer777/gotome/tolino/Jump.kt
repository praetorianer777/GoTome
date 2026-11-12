package io.github.praetorianer777.gotome.tolino

import kotlin.math.floor

/** Where GOtome last had the person in a book, as kept from the last sync. */
data class Remote(val fraction: Double, val clientId: String, val updatedAt: String)

/**
 * Whether to offer a jump when a book opens on the Tolino: when someone read
 * further elsewhere, by more than a page. Pages are the reader's own ("13 / 510"),
 * which are Adobe's pages counted from 1, so a share of the book is a page number.
 */
object Jump {
    fun page(fraction: Double, total: Int): Int = (floor(fraction * total).toInt() + 1).coerceIn(1, total)

    /** The page to offer, or null. [position] is the reader's, in Adobe's pages from 0. */
    fun offer(position: Double, total: Int, remote: Remote?, ownClientId: String): Int? {
        if (remote == null || total <= 0 || remote.clientId == ownClientId) return null
        val target = page(remote.fraction, total)
        val current = (floor(position).toInt() + 1).coerceIn(1, total)
        return if (target > current + 1) target else null
    }
}
