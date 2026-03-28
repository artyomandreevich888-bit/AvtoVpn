package com.autovpn

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.widget.Button
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat

class MainActivity : AppCompatActivity() {

    companion object {
        var instance: MainActivity? = null
    }

    private lateinit var btnConnect: Button
    private lateinit var statusText: TextView
    private lateinit var serverText: TextView
    private var isConnected = false

    private val vpnPermission = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == Activity.RESULT_OK) {
            startVpn()
        }
    }

    private val notificationPermission = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) {
        requestVpnPermission()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        instance = this
        setContentView(R.layout.activity_main)

        btnConnect = findViewById(R.id.btn_connect)
        statusText = findViewById(R.id.status_text)
        serverText = findViewById(R.id.server_text)

        if (AutoVpnService.instance != null) {
            isConnected = true
            btnConnect.text = getString(R.string.disconnect)
        }

        btnConnect.setOnClickListener {
            if (isConnected) {
                stopVpn()
            } else {
                requestPermissions()
            }
        }
    }

    private fun requestPermissions() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            if (ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
                != PackageManager.PERMISSION_GRANTED
            ) {
                notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
                return
            }
        }
        requestVpnPermission()
    }

    private fun requestVpnPermission() {
        val intent = VpnService.prepare(this)
        if (intent != null) {
            vpnPermission.launch(intent)
        } else {
            startVpn()
        }
    }

    private fun startVpn() {
        val intent = Intent(this, AutoVpnService::class.java).apply {
            action = AutoVpnService.ACTION_START
        }
        ContextCompat.startForegroundService(this, intent)
        isConnected = true
        btnConnect.text = getString(R.string.connecting)
        btnConnect.isEnabled = false
    }

    private fun stopVpn() {
        val intent = Intent(this, AutoVpnService::class.java).apply {
            action = AutoVpnService.ACTION_STOP
        }
        startService(intent)
        isConnected = false
        btnConnect.text = getString(R.string.connect)
        statusText.text = ""
        serverText.text = ""
    }

    // Called from Go status callback (via runOnUiThread)
    fun updateUI(state: Long, server: String, delayMs: Long, alive: Long, total: Long, error: String) {
        when (state) {
            1L -> { // Fetching
                btnConnect.text = getString(R.string.connecting)
                btnConnect.isEnabled = false
                statusText.text = "Downloading configs..."
                serverText.text = ""
            }
            2L -> { // Starting
                statusText.text = "Starting..."
            }
            3L -> { // Connected
                isConnected = true
                btnConnect.text = getString(R.string.disconnect)
                btnConnect.isEnabled = true
                if (server.isNotEmpty()) {
                    statusText.text = "$server (${delayMs}ms)"
                    serverText.text = "$alive / $total servers available"
                } else {
                    statusText.text = "Connected"
                    serverText.text = "$total servers"
                }
            }
            4L -> { // Error
                isConnected = false
                btnConnect.text = getString(R.string.connect)
                btnConnect.isEnabled = true
                statusText.text = "Error: $error"
                serverText.text = ""
            }
            0L -> { // Disconnected
                updateDisconnected()
            }
        }
    }

    fun updateDisconnected() {
        isConnected = false
        btnConnect.text = getString(R.string.connect)
        btnConnect.isEnabled = true
        statusText.text = ""
        serverText.text = ""
    }

    override fun onDestroy() {
        instance = null
        super.onDestroy()
    }
}
