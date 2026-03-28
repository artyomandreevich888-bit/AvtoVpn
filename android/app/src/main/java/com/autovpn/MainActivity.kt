package com.autovpn

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.content.pm.PackageManager
import android.content.res.ColorStateList
import android.graphics.Color
import android.net.VpnService
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.View
import android.widget.TextView
import androidx.activity.result.contract.ActivityResultContracts
import androidx.appcompat.app.AppCompatActivity
import androidx.core.content.ContextCompat
import com.google.android.material.button.MaterialButton
import mobile.Mobile

class MainActivity : AppCompatActivity() {

    companion object {
        var instance: MainActivity? = null
        private const val GREEN = "#30D060"
        private const val ORANGE = "#F0A030"
        private const val RED = "#E04040"
        private const val IDLE_STROKE = "#333333"
        private const val IDLE_TEXT = "#888888"
    }

    private lateinit var btnConnect: MaterialButton
    private lateinit var statusText: TextView
    private lateinit var serverText: TextView
    private lateinit var ipText: TextView
    private lateinit var uptimeText: TextView
    private lateinit var speedText: TextView
    private lateinit var errorText: TextView
    private lateinit var configInfo: View
    private lateinit var configSourceText: TextView
    private lateinit var serversHeader: View
    private lateinit var serversHeaderText: TextView
    private lateinit var serversToggle: TextView
    private lateinit var serversList: android.widget.LinearLayout
    private var serversExpanded = false
    private lateinit var svcYoutube: TextView
    private lateinit var svcInstagram: TextView
    private lateinit var svcGithub: TextView

    private var isConnected = false
    private var connectedSince = 0L
    private var totalDown = 0L
    private var totalUp = 0L

    private val handler = Handler(Looper.getMainLooper())

    private var tickCount = 0

    // Uptime + traffic polling every second, servers every 5s
    private val tickRunnable = object : Runnable {
        override fun run() {
            if (!isConnected) return
            updateUptime()
            pollTraffic()
            tickCount++
            if (tickCount % 5 == 0) pollServers()
            handler.postDelayed(this, 1000)
        }
    }

    private val vpnPermission = registerForActivityResult(
        ActivityResultContracts.StartActivityForResult()
    ) { result ->
        if (result.resultCode == Activity.RESULT_OK) startVpn()
    }

    private val notificationPermission = registerForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { requestVpnPermission() }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        instance = this
        setContentView(R.layout.activity_main)

        btnConnect = findViewById(R.id.btn_connect)
        statusText = findViewById(R.id.status_text)
        serverText = findViewById(R.id.server_text)
        ipText = findViewById(R.id.ip_text)
        uptimeText = findViewById(R.id.uptime_text)
        speedText = findViewById(R.id.speed_text)
        errorText = findViewById(R.id.error_text)
        configInfo = findViewById(R.id.config_info)
        configSourceText = findViewById(R.id.config_source_text)
        serversHeader = findViewById(R.id.servers_header)
        serversHeaderText = findViewById(R.id.servers_header_text)
        serversToggle = findViewById(R.id.servers_toggle)
        serversList = findViewById(R.id.servers_list)
        serversToggle.setOnClickListener {
            serversExpanded = !serversExpanded
            serversList.visibility = if (serversExpanded) View.VISIBLE else View.GONE
            serversToggle.text = if (serversExpanded) "HIDE" else "SHOW"
        }
        svcYoutube = findViewById(R.id.svc_youtube)
        svcInstagram = findViewById(R.id.svc_instagram)
        svcGithub = findViewById(R.id.svc_github)

        if (AutoVpnService.instance != null) {
            isConnected = true
            setButtonState(connected = true)
        }

        btnConnect.setOnClickListener {
            if (isConnected) stopVpn() else requestPermissions()
        }
    }

    private fun requestPermissions() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
            != PackageManager.PERMISSION_GRANTED
        ) {
            notificationPermission.launch(Manifest.permission.POST_NOTIFICATIONS)
            return
        }
        requestVpnPermission()
    }

    private fun requestVpnPermission() {
        val intent = VpnService.prepare(this)
        if (intent != null) vpnPermission.launch(intent) else startVpn()
    }

    private fun startVpn() {
        ContextCompat.startForegroundService(this,
            Intent(this, AutoVpnService::class.java).apply { action = AutoVpnService.ACTION_START })
        // Do NOT set isConnected here — wait for StateConnected callback
        // to trigger verifyConnection()
        setButtonState(connecting = true)
        setBadgesChecking()
    }

    private fun stopVpn() {
        startService(Intent(this, AutoVpnService::class.java).apply { action = AutoVpnService.ACTION_STOP })
        updateDisconnected()
    }

    // Called from Go status callback via AutoVpnService
    fun updateUI(state: Long, server: String, delayMs: Long, alive: Long, total: Long, error: String) {
        when (state) {
            1L -> { // Fetching + pre-validation (before TUN)
                setButtonState(connecting = true)
                statusText.text = if (server.isNotEmpty()) server else "Downloading configs..."
                if (total > 0) serverText.text = "$alive / $total alive" else serverText.text = ""
                errorText.visibility = View.GONE
            }
            2L -> { // Starting engine
                statusText.text = if (server.isNotEmpty()) server else "Starting engine..."
            }
            3L -> { // Connected — servers already pre-validated
                if (!isConnected) {
                    isConnected = true
                    setButtonState(connected = true)
                    connectedSince = System.currentTimeMillis()
                    totalDown = 0; totalUp = 0
                    handler.post(tickRunnable)
                    errorText.visibility = View.GONE
                    fetchConfigInfo()
                    statusText.text = if (server.isNotEmpty()) server else "Connected"
                    serverText.text = if (total > 0) "$alive / $total servers" else ""
                    // Check IP in background, poll servers
                    quickVerifyIP()
                    runServiceChecks()
                    pollServers()
                } else {
                    // Normal connected updates (from pollStatus)
                    errorText.visibility = View.GONE
                    if (server.isNotEmpty()) {
                        statusText.text = "$server  ${delayMs}ms"
                        serverText.text = "$alive / $total servers"
                    }
                }
            }
            4L -> { // Error — shown without TUN (no kill switch)
                isConnected = false
                setButtonState(error = true)
                statusText.text = ""
                serverText.text = ""
                errorText.text = error
                errorText.visibility = View.VISIBLE
                resetStats()
            }
            0L -> updateDisconnected()
        }
    }

    fun updateDisconnected() {
        isConnected = false
        setButtonState()
        statusText.text = ""
        serverText.text = ""
        errorText.visibility = View.GONE
        resetStats()
    }

    // --- Stats polling ---

    private fun updateUptime() {
        if (connectedSince == 0L) return
        val secs = (System.currentTimeMillis() - connectedSince) / 1000
        val h = secs / 3600; val m = (secs % 3600) / 60; val s = secs % 60
        uptimeText.text = if (h > 0) String.format("%d:%02d:%02d", h, m, s)
                          else String.format("%d:%02d", m, s)
    }

    private fun pollTraffic() {
        Thread {
            try {
                val raw = Mobile.getTraffic() // "up,down"
                val parts = raw.split(",")
                if (parts.size == 2) {
                    val up = parts[0].toLongOrNull() ?: 0
                    val down = parts[1].toLongOrNull() ?: 0
                    totalUp += up; totalDown += down
                    runOnUiThread {
                        speedText.text = "${formatBytes(down)}/s ↓  ${formatBytes(up)}/s ↑"
                    }
                }
            } catch (_: Exception) {}
        }.start()
    }

    private fun quickVerifyIP() {
        Thread {
            val ip = try { Mobile.getExternalIP() } catch (_: Exception) { "" }
            runOnUiThread {
                if (ip.isNotEmpty()) ipText.text = ip
            }
        }.start()
    }

    private fun fetchConfigInfo() {
        Thread {
            try {
                val raw = Mobile.getConfigInfo() // "source,alive,total,cacheAgeSec"
                val parts = raw.split(",")
                if (parts.size == 4) {
                    val source = parts[0]
                    val alive = parts[1]
                    val total = parts[2]
                    val ageSec = parts[3].toLongOrNull() ?: 0
                    val label = when (source) {
                        "network" -> "\u2601 $alive/$total servers (fresh)"
                        "cache" -> "\u23F0 $alive/$total servers (cached ${formatDuration(ageSec)})"
                        "embedded" -> "\u26A0 $alive/$total servers (built-in fallback)"
                        else -> "$alive/$total servers"
                    }
                    runOnUiThread {
                        configSourceText.text = label
                        configInfo.visibility = View.VISIBLE
                    }
                }
            } catch (_: Exception) {}
        }.start()
    }

    private fun formatDuration(sec: Long): String = when {
        sec < 60 -> "just now"
        sec < 3600 -> "${sec / 60}m ago"
        sec < 86400 -> "${sec / 3600}h ago"
        else -> "${sec / 86400}d ago"
    }

    private fun runServiceChecks() {
        setBadgesChecking()
        Thread {
            val result = Mobile.checkServices()
            runOnUiThread {
                if (result.isEmpty()) {
                    setBadge(svcYoutube, "ok")
                    setBadge(svcInstagram, "ok")
                    setBadge(svcGithub, "ok")
                } else {
                    setBadge(svcYoutube, "fail", result)
                    setBadge(svcInstagram, "idle")
                    setBadge(svcGithub, "idle")
                }
            }
        }.start()
    }

    private fun pollServers() {
        Thread {
            try {
                val raw = Mobile.getServerList()
                if (raw.isBlank()) return@Thread
                val lines = raw.trim().split("\n")
                runOnUiThread { renderServerLines(lines) }
            } catch (_: Exception) {}
        }.start()
    }

    private fun renderServerLines(lines: List<String>) {
        serversList.removeAllViews()
        var aliveCount = 0
        var deadCount = 0
        for (line in lines) {
            val p = line.split(",")
            if (p.size < 4) continue
            val name = p[0]
            val delay = p[1].toIntOrNull() ?: 0
            val alive = p[2] == "1"
            val active = p[3] == "1"
            if (alive) aliveCount++ else deadCount++

            val row = android.widget.LinearLayout(this).apply {
                orientation = android.widget.LinearLayout.HORIZONTAL
                gravity = android.view.Gravity.CENTER_VERTICAL
                setPadding(dp(12), dp(6), dp(12), dp(6))
                layoutParams = android.widget.LinearLayout.LayoutParams(
                    android.widget.LinearLayout.LayoutParams.MATCH_PARENT,
                    android.widget.LinearLayout.LayoutParams.WRAP_CONTENT
                ).apply { bottomMargin = dp(4) }
                setBackgroundResource(R.drawable.bg_service_row)
            }

            // Active indicator
            if (active) {
                row.addView(TextView(this).apply {
                    text = "\u25B6 "
                    setTextColor(Color.parseColor(GREEN))
                    textSize = 10f
                })
            }

            // Server name
            row.addView(TextView(this).apply {
                text = name.replace("server-", "srv")
                setTextColor(if (alive) Color.parseColor("#AAAAAA") else Color.parseColor("#444444"))
                textSize = 11f
                layoutParams = android.widget.LinearLayout.LayoutParams(0, android.widget.LinearLayout.LayoutParams.WRAP_CONTENT, 1f)
                maxLines = 1
                ellipsize = android.text.TextUtils.TruncateAt.END
            })

            // Delay badge
            row.addView(TextView(this).apply {
                text = if (alive) "${delay}ms" else "dead"
                setTextColor(Color.parseColor(
                    when {
                        !alive -> RED
                        delay < 300 -> GREEN
                        delay < 800 -> ORANGE
                        else -> RED
                    }
                ))
                textSize = 11f
                setBackgroundResource(
                    when {
                        !alive -> R.drawable.bg_badge_fail
                        delay < 300 -> R.drawable.bg_badge_ok
                        delay < 800 -> R.drawable.bg_badge_checking
                        else -> R.drawable.bg_badge_fail
                    }
                )
                setPadding(dp(8), dp(2), dp(8), dp(2))
            })

            serversList.addView(row)
        }
        serversHeader.visibility = View.VISIBLE
        serversHeaderText.text = "\u2714 $aliveCount alive    \u2716 $deadCount dead    \u2211 ${lines.size} total"
    }

    private fun dp(v: Int): Int = (v * resources.displayMetrics.density).toInt()

    private fun resetStats() {
        handler.removeCallbacks(tickRunnable)
        connectedSince = 0; totalDown = 0; totalUp = 0
        ipText.text = "--"
        uptimeText.text = "--"
        speedText.text = "--"
        configInfo.visibility = View.GONE
        serversHeader.visibility = View.GONE
        serversList.removeAllViews()
        serversList.visibility = View.GONE
        serversExpanded = false
        tickCount = 0
        resetBadges()
    }

    // --- Button states ---

    private fun setButtonState(connected: Boolean = false, connecting: Boolean = false, error: Boolean = false) {
        val text: String
        val color: String
        val enabled: Boolean
        when {
            connecting -> { text = getString(R.string.connecting); color = ORANGE; enabled = false }
            connected -> { text = getString(R.string.disconnect); color = GREEN; enabled = true }
            error -> { text = getString(R.string.connect); color = RED; enabled = true }
            else -> { text = getString(R.string.connect); color = IDLE_TEXT; enabled = true }
        }
        btnConnect.text = text
        btnConnect.setTextColor(Color.parseColor(color))
        btnConnect.strokeColor = ColorStateList.valueOf(Color.parseColor(
            if (connecting || connected || error) color else IDLE_STROKE
        ))
        btnConnect.isEnabled = enabled
    }

    // --- Service badges ---

    private fun setBadge(tv: TextView, state: String, text: String? = null) {
        val (label, color, bg) = when (state) {
            "ok" -> Triple(text ?: "OK", GREEN, R.drawable.bg_badge_ok)
            "fail" -> Triple(text ?: "FAIL", RED, R.drawable.bg_badge_fail)
            "checking" -> Triple("...", ORANGE, R.drawable.bg_badge_checking)
            else -> Triple("--", "#555555", R.drawable.bg_badge_idle)
        }
        tv.text = label
        tv.setTextColor(Color.parseColor(color))
        tv.setBackgroundResource(bg)
    }

    private fun setBadgesChecking() {
        setBadge(svcYoutube, "checking")
        setBadge(svcInstagram, "checking")
        setBadge(svcGithub, "checking")
    }

    private fun resetBadges() {
        setBadge(svcYoutube, "idle")
        setBadge(svcInstagram, "idle")
        setBadge(svcGithub, "idle")
    }

    // --- Helpers ---

    private fun formatBytes(bytes: Long): String = when {
        bytes >= 1_048_576 -> String.format("%.1fMB", bytes / 1_048_576.0)
        bytes >= 1024 -> String.format("%.0fKB", bytes / 1024.0)
        else -> "${bytes}B"
    }

    override fun onDestroy() {
        instance = null
        handler.removeCallbacks(tickRunnable)
        super.onDestroy()
    }
}
