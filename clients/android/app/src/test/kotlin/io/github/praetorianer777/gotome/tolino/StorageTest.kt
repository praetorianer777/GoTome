package io.github.praetorianer777.gotome.tolino

import org.junit.Assert.assertEquals
import org.junit.Test

class StorageTest {
    private val id = "1a2b3c4d-0000-4000-8000-000000000000"

    @Test
    fun namesAFileInPlainLetters() {
        assertEquals(
            "Hobb_Robin-Der_Weitseher-1a2b3c4d.epub",
            Storage.fileName(id, "Der Weitseher", listOf("Hobb, Robin"), "EPUB"),
        )
    }

    @Test
    fun dropsAccentsAndSpellsOutTheSharpS() {
        assertEquals("Muller-Strasse_der_Traume-1a2b3c4d.pdf", Storage.fileName(id, "Straße der Träume!", listOf("Müller"), "pdf"))
    }

    @Test
    fun aBookWithoutAuthorOrLatinTitleStillGetsAName() {
        assertEquals("1a2b3c4d.epub", Storage.fileName(id, "東京", emptyList(), "epub"))
    }

    @Test
    fun keepsLongTitlesShort() {
        val name = Storage.fileName(id, "A".repeat(200), listOf("B".repeat(100)), "epub")
        assertEquals(30 + 1 + 60 + 1 + 8 + 5, name.length)
    }

    @Test
    fun searchWordsIgnoreCaseAccentsAndPunctuation() {
        assertEquals("muller strasse 3", plain("  Müller: STRASSE—3 "))
        assertEquals("strasse", plain("Straße"))
    }
}
