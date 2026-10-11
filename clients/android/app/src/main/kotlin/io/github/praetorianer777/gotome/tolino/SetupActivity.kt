package io.github.praetorianer777.gotome.tolino

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Bundle
import android.widget.Button
import android.widget.EditText
import android.widget.TextView
import java.text.DateFormat
import java.util.Date

/** Where GOtome is, who signs in, which collection is the device's, and whether the app can see the reader. */
class SetupActivity : Activity() {
    private lateinit var prefs: Prefs
    private lateinit var server: EditText
    private lateinit var username: EditText
    private lateinit var password: EditText
    private lateinit var collection: EditText
    private lateinit var status: TextView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        prefs = Prefs(this)
        setContentView(R.layout.activity_setup)
        server = findViewById(R.id.server)
        username = findViewById(R.id.username)
        password = findViewById(R.id.password)
        collection = findViewById(R.id.collection)
        status = findViewById(R.id.status)
        server.setText(prefs.server.ifEmpty { "http://" })
        server.setSelection(server.text.length)
        username.setText(prefs.username)
        collection.setText(prefs.collectionName)
        findViewById<Button>(R.id.sign_in).setOnClickListener { signIn() }
        findViewById<Button>(R.id.store).setOnClickListener {
            startActivity(Intent(this, StoreActivity::class.java))
            finish()
        }
        val storage = arrayOf(Manifest.permission.READ_EXTERNAL_STORAGE, Manifest.permission.WRITE_EXTERNAL_STORAGE)
        if (storage.any { checkSelfPermission(it) != PackageManager.PERMISSION_GRANTED }) requestPermissions(storage, 1)
    }

    override fun onResume() {
        super.onResume()
        showStatus()
    }

    private fun signIn() {
        val name = collection.text.toString().trim().ifEmpty { "Tolino" }
        status.text = getString(R.string.signing_in)
        Thread {
            val result = runCatching {
                if (!Network.online(this)) throw ApiException(0, getString(R.string.offline))
                Api(prefs).login(server.text.toString(), username.text.toString().trim(), password.text.toString())
                prefs.collectionName = name
                prefs.collectionId = null
                LauncherBook.ensure(this)
                Sync(this).run { step -> runOnUiThread { status.text = step } }
            }
            runOnUiThread {
                if (isFinishing) return@runOnUiThread
                password.setText("")
                result.onSuccess {
                    ReaderWatcher.start(this)
                    SyncJob.schedule(this)
                }
                showStatus(result.exceptionOrNull()?.message)
            }
        }.start()
    }

    private fun showStatus(error: String? = null) {
        val lines = mutableListOf<String>()
        error?.let { lines += getString(R.string.sign_in_failed, it) }
        lines += if (prefs.signedIn) getString(R.string.signed_in_as, prefs.username, prefs.server)
        else getString(R.string.signed_out)
        lines += if (ReaderWatcher.canReadLog(this)) getString(R.string.log_granted)
        else getString(R.string.log_missing, packageName)
        lines += if (JumpService.enabled(this)) getString(R.string.jump_on) else getString(R.string.jump_off, packageName)
        lines += if (prefs.readerSeenAt == 0L) getString(R.string.reader_never)
        else getString(R.string.reader_seen, DateFormat.getDateTimeInstance(DateFormat.SHORT, DateFormat.SHORT).format(Date(prefs.readerSeenAt)))
        prefs.lastError?.let { lines += getString(R.string.sync_failed, it) }
        status.text = lines.joinToString("\n\n")
    }
}
