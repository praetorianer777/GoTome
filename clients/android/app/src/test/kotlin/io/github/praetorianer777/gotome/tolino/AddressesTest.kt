package io.github.praetorianer777.gotome.tolino

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class AddressesTest {
    private val home = "http://192.168.1.5:8080"
    private val road = "https://books.example.org"

    @Test
    fun keepsTheEnteredAddressFirstAndNoneTwice() {
        assertEquals(listOf(road, home), Addresses.order("$road/", listOf(home, road), null))
    }

    @Test
    fun putsTheOneThatWorkedLastFirst() {
        assertEquals(listOf(road, home), Addresses.order(home, listOf(road), road))
    }

    @Test
    fun ignoresALastGoodThatIsNoLongerListed() {
        assertEquals(listOf(home, road), Addresses.order(home, listOf(road), "http://gone.example.org"))
    }

    @Test
    fun dropsWhatIsNoHttpAddress() {
        assertEquals(listOf(home), Addresses.order(home, listOf("ftp://x.example.org", "books", "https://u:p@x.example.org"), null))
    }

    @Test
    fun fallsBackToTheNextAddressThatAnswers() {
        val tried = mutableListOf<String>()
        val chosen = Addresses.choose(listOf(home, road)) { tried += it; it == road }
        assertEquals(road, chosen)
        assertEquals(listOf(home, road), tried)
    }

    @Test
    fun stopsAtTheFirstThatAnswers() {
        val tried = mutableListOf<String>()
        assertEquals(home, Addresses.choose(listOf(home, road)) { tried += it; true })
        assertEquals(listOf(home), tried)
    }

    @Test
    fun choosesNoneWhenNoneAnswers() {
        assertNull(Addresses.choose(listOf(home, road)) { false })
    }

    @Test
    fun keepsAndRestoresTheList() {
        assertEquals(listOf(home, road), Addresses.split(Addresses.join(listOf(home, road))))
        assertEquals(emptyList<String>(), Addresses.split(""))
    }

    @Test
    fun readsTheAddressesOfTheDeviceAnswer() {
        val body = """{"device":{"name":"Tolino"},"username":"steve","addresses":["$home","ftp://x","$road"]}"""
        assertEquals(ThisDevice("Tolino", "steve", listOf(home, road)), Json.thisDevice(body))
    }
}
