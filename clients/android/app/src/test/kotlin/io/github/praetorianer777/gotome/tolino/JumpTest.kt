package io.github.praetorianer777.gotome.tolino

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class JumpTest {
    private val own = "tolino-ab12cd34"
    private fun web(fraction: Double) = Remote(fraction, "web", "2026-10-11T15:00:00Z")

    @Test
    fun aShareOfTheBookIsTheReadersPageCountedFromOne() {
        assertEquals(1, Jump.page(0.0, 510))
        assertEquals(240, Jump.page(0.47, 510))
        assertEquals(510, Jump.page(1.0, 510))
    }

    @Test
    fun offersThePageReadFurtherElsewhere() {
        // The reader at "13 / 510", the browser at 47 %.
        assertEquals(240, Jump.offer(12.55, 510, web(0.47), own))
    }

    @Test
    fun offersNothingForAPageOrLess() {
        assertNull(Jump.offer(12.55, 510, web(13.5 / 510), own))
        assertNull(Jump.offer(12.55, 510, web(0.01), own))
    }

    @Test
    fun offersNothingForThisTolinosOwnPosition() {
        assertNull(Jump.offer(12.55, 510, Remote(0.47, own, "t"), own))
    }

    @Test
    fun offersNothingWithoutAPositionOrPages() {
        assertNull(Jump.offer(12.55, 510, null, own))
        assertNull(Jump.offer(12.55, 0, web(0.47), own))
    }
}
