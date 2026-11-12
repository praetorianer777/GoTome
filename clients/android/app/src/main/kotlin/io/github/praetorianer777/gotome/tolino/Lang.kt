package io.github.praetorianer777.gotome.tolino

import android.content.Context
import android.content.res.Configuration
import java.util.Locale

/**
 * The language the app speaks, chosen on its settings page: English unless
 * German was chosen, whatever the Tolino itself is set to. Every activity,
 * service and receiver reads its texts through [wrap].
 */
object Lang {
    const val ENGLISH = "en"
    const val GERMAN = "de"

    fun wrap(base: Context): Context {
        val config = Configuration(base.resources.configuration)
        config.setLocale(Locale(Prefs(base).language))
        return base.createConfigurationContext(config)
    }
}
