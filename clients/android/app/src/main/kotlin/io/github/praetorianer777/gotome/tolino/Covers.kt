package io.github.praetorianer777.gotome.tolino

import android.content.Context
import android.graphics.Bitmap
import android.graphics.BitmapFactory
import android.os.Handler
import android.os.Looper
import android.widget.ImageView
import java.io.File
import java.util.concurrent.Executors

/** Small covers, fetched once and kept on the device, so a page of the store shows them at once. */
class Covers(context: Context) {
    private val dir = File(context.filesDir, "covers").apply { mkdirs() }
    private val api = Api(Prefs(context))
    private val main = Handler(Looper.getMainLooper())

    fun show(bookId: String, view: ImageView, size: String = "small") {
        view.tag = bookId
        view.setImageDrawable(null)
        pool.execute {
            val file = File(dir, "$bookId-$size.jpg")
            if (!file.exists()) {
                runCatching { api.bytes("/books/$bookId/covers/$size") }.getOrNull()?.let { file.writeBytes(it) }
            }
            val bitmap = if (file.exists()) decode(file, view.layoutParams.height.coerceAtLeast(200)) else null
            main.post { if (view.tag == bookId) view.setImageBitmap(bitmap) }
        }
    }

    private fun decode(file: File, height: Int): Bitmap? {
        val bounds = BitmapFactory.Options().apply { inJustDecodeBounds = true }
        BitmapFactory.decodeFile(file.path, bounds)
        var sample = 1
        while (bounds.outHeight / (sample * 2) >= height) sample *= 2
        return BitmapFactory.decodeFile(file.path, BitmapFactory.Options().apply { inSampleSize = sample })
    }

    companion object {
        private val pool = Executors.newFixedThreadPool(2)
    }
}
