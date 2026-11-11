package io.github.praetorianer777.gotome.tolino

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class UpdatesTest {
    private val pkg = "io.github.praetorianer777.gotome.tolino"
    private val key = setOf("3082")

    private fun release(tag: String, vararg assets: Pair<String, String>, extra: String = "") =
        """{"tag_name":"$tag"$extra,"assets":[${assets.joinToString(",") { """{"name":"${it.first}","browser_download_url":"${it.second}"}""" }}]}"""

    @Test
    fun findsTheApkOfTheLatestRelease() {
        val body = release(
            "v0.2.0",
            "notes.md" to "https://github.com/x/notes.md",
            "gotome-tolino-0.2.0.apk" to "https://github.com/praetorianer777/GoTome/releases/download/v0.2.0/gotome-tolino-0.2.0.apk",
        )
        assertEquals(
            Release("0.2.0", "https://github.com/praetorianer777/GoTome/releases/download/v0.2.0/gotome-tolino-0.2.0.apk"),
            Updates.latest(body),
        )
    }

    @Test
    fun aReleaseWithoutItsApkOrNotPlainIsNone() {
        assertNull(Updates.latest(release("v0.2.0", "gotome-tolino-0.1.0.apk" to "https://a/b.apk")))
        assertNull(Updates.latest(release("v0.2.0", "gotome-tolino-0.2.0.apk" to "http://a/b.apk")))
        assertNull(Updates.latest(release("v0.2.0-rc1", "gotome-tolino-0.2.0-rc1.apk" to "https://a/b.apk")))
        assertNull(Updates.latest(release("v0.2.0", "gotome-tolino-0.2.0.apk" to "https://a/b.apk", extra = ""","prerelease":true""")))
        assertNull(Updates.latest("""{"message":"Not Found"}"""))
    }

    @Test
    fun ofSeveralReleasesTheNewestWithItsApkWins() {
        fun entry(v: String, apk: Boolean) =
            release("v$v", *(if (apk) arrayOf("gotome-tolino-$v.apk" to "https://a/$v.apk") else emptyArray()))
        val body = "[" + listOf(entry("0.20.0", false), entry("0.9.0", true), entry("0.10.0", true), entry("0.8.0", true)).joinToString(",") + "]"
        assertEquals(Release("0.10.0", "https://a/0.10.0.apk"), Updates.latest(body))
        assertNull(Updates.latest("[" + entry("0.20.0", false) + "]"))
        assertNull(Updates.latest("[]"))
    }

    @Test
    fun comparesVersionsByTheirNumbers() {
        assertTrue(Updates.newer("0.10.0", "0.9.9"))
        assertTrue(Updates.newer("1.0.0", "0.99.99"))
        assertFalse(Updates.newer("0.1.0", "0.1.0"))
        assertFalse(Updates.newer("0.1.0", "0.2.0"))
        assertFalse(Updates.newer("garbage", "0.1.0"))
    }

    @Test
    fun handsOnlyALaterBuildOfThisAppWithItsKeyToTheInstaller() {
        assertNull(Updates.refusal(pkg, 100, key, pkg, 200, key))
        assertEquals(R.string.update_other_key, Updates.refusal(pkg, 100, key, pkg, 200, setOf("3083")))
        assertEquals(R.string.update_other_key, Updates.refusal(pkg, 100, key, pkg, 200, emptySet()))
        assertEquals(R.string.update_other_key, Updates.refusal(pkg, 100, emptySet(), pkg, 200, emptySet()))
        assertEquals(R.string.update_other_key, Updates.refusal(pkg, 100, key, pkg, 200, key + "3083"))
        assertEquals(R.string.update_not_newer, Updates.refusal(pkg, 100, key, pkg, 100, key))
        assertEquals(R.string.update_not_app, Updates.refusal(pkg, 100, key, "com.example.other", 200, key))
        assertEquals(R.string.update_not_app, Updates.refusal(pkg, 100, key, null, 0, emptySet()))
    }
}
