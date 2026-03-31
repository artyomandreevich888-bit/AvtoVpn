package com.autovpn

import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.net.Uri
import androidx.core.content.ContextCompat
import android.webkit.JavascriptInterface
import mobile.Mobile
import org.json.JSONArray
import org.json.JSONObject

/**
 * JavaScript interface exposed to the WebView as window.AndroidBridge.
 * All methods annotated with @JavascriptInterface run on a background thread.
 */
class AndroidBridge(private val context: Context) {

    private val prefs: SharedPreferences =
        context.getSharedPreferences("autovpn_prefs", Context.MODE_PRIVATE)

    // ── VPN control ──────────────────────────────────────────────────────────

    @JavascriptInterface
    fun connectAsync() {
        val prepare = android.net.VpnService.prepare(context)
        if (prepare != null) {
            // Need to show system permission dialog — delegate to Activity
            MainActivity.instance?.runOnUiThread {
                MainActivity.instance?.requestPermissionsAndStart()
            }
        } else {
            ContextCompat.startForegroundService(context,
                Intent(context, AutoVpnService::class.java).apply { action = AutoVpnService.ACTION_START })
        }
    }

    @JavascriptInterface
    fun disconnect() {
        context.startService(
            Intent(context, AutoVpnService::class.java).apply { action = AutoVpnService.ACTION_STOP })
    }

    // ── Status ────────────────────────────────────────────────────────────────

    /** Returns current VPN status as JSON string (fast, no network). */
    @JavascriptInterface
    fun getStatusJSON(): String = try { Mobile.getStatusJSON() } catch (_: Exception) {
        """{"State":"disconnected","Server":"","Delay":0,"AliveCount":0,"TotalCount":0,"Error":""}"""
    }

    // ── Traffic ───────────────────────────────────────────────────────────────

    /** Returns "up,down" bytes/sec (fast). */
    @JavascriptInterface
    fun getTraffic(): String = try { Mobile.getTraffic() } catch (_: Exception) { "0,0" }

    // ── Location info (async) ─────────────────────────────────────────────────

    @Volatile private var locationResult: String? = null
    @Volatile private var locationPending = false

    @JavascriptInterface
    fun triggerLocationInfo() {
        if (locationPending) return
        locationPending = true
        locationResult = null
        Thread {
            locationResult = try { Mobile.getLocationInfoJSON() } catch (_: Exception) { "" }
            locationPending = false
        }.start()
    }

    @JavascriptInterface
    fun getLocationInfoResult(): String {
        val r = locationResult
        if (r != null && !locationPending) {
            locationResult = null   // consume
            return r
        }
        return ""
    }

    // ── Service checks (async) ────────────────────────────────────────────────

    @Volatile private var servicesResult: String? = null
    @Volatile private var servicesPending = false

    @JavascriptInterface
    fun triggerCheckServices() {
        if (servicesPending) return
        servicesPending = true
        servicesResult = null
        Thread {
            servicesResult = try { Mobile.checkServicesJSON() } catch (_: Exception) { "[]" }
            servicesPending = false
        }.start()
    }

    @JavascriptInterface
    fun getServicesResult(): String {
        val r = servicesResult
        if (r != null && !servicesPending) {
            servicesResult = null   // consume
            return r
        }
        return ""
    }

    // ── Server list ───────────────────────────────────────────────────────────

    @JavascriptInterface
    fun getServerListJSON(): String = try { Mobile.getServerListJSON() } catch (_: Exception) { "[]" }

    // ── Select server (async) ─────────────────────────────────────────────────

    @Volatile private var selectServerDone = false
    @Volatile private var selectServerPending = false

    @JavascriptInterface
    fun triggerSelectServer(tag: String) {
        selectServerDone = false
        selectServerPending = true
        Thread {
            try { Mobile.selectServerByTag(tag) } catch (_: Exception) {}
            selectServerDone = true
            selectServerPending = false
        }.start()
    }

    @JavascriptInterface
    fun getSelectServerDone(): Boolean {
        if (selectServerDone) {
            selectServerDone = false
            return true
        }
        return false
    }

    // ── Refresh servers ───────────────────────────────────────────────────────

    @JavascriptInterface
    fun refreshServers() {
        // Stop and restart VPN to re-fetch servers
        if (AutoVpnService.instance != null) {
            context.startService(
                Intent(context, AutoVpnService::class.java).apply { action = AutoVpnService.ACTION_STOP })
            Thread.sleep(800)
            ContextCompat.startForegroundService(context,
                Intent(context, AutoVpnService::class.java).apply { action = AutoVpnService.ACTION_START })
        }
    }

    // ── History ───────────────────────────────────────────────────────────────

    @JavascriptInterface
    fun getHistoryJSON(): String = prefs.getString("history", "[]") ?: "[]"

    @JavascriptInterface
    fun saveRecord(server: String, country: String, duration: Long) {
        val arr = try { JSONArray(prefs.getString("history", "[]")) } catch (_: Exception) { JSONArray() }
        val rec = JSONObject().apply {
            put("Server", server)
            put("Country", country)
            put("Duration", duration)
            put("Time", java.text.SimpleDateFormat("dd.MM HH:mm", java.util.Locale.getDefault())
                .format(java.util.Date()))
        }
        // Prepend new record
        val newArr = JSONArray()
        newArr.put(rec)
        for (i in 0 until minOf(arr.length(), 49)) newArr.put(arr.get(i))
        prefs.edit().putString("history", newArr.toString()).apply()
    }

    // ── Settings ──────────────────────────────────────────────────────────────

    @JavascriptInterface
    fun getAutoConnect(): Boolean = prefs.getBoolean("autoConnect", false)

    @JavascriptInterface
    fun setAutoConnect(v: Boolean) { prefs.edit().putBoolean("autoConnect", v).apply() }

    // -- Ad banner (async) --

    @Volatile private var adBannerResult: String? = null
    @Volatile private var adBannerPending = false

    @JavascriptInterface
    fun triggerAdBanner() {
        if (adBannerPending) return
        adBannerPending = true
        adBannerResult = null
        Thread {
            adBannerResult = try { Mobile.getAdBannerJSON() ?: "" } catch (_: Exception) { "" }
            adBannerPending = false
        }.start()
    }

    @JavascriptInterface
    fun getAdBannerResult(): String {
        if (adBannerPending) return "__pending__"
        val r = adBannerResult
        if (r != null) { adBannerResult = null; return r }
        return "__pending__"
    }


    // ── File integrity check ──────────────────────────────────────────────────

    @JavascriptInterface
    fun checkIntegrity(): Boolean {
        return try {
            val expectedHashes = mapOf(
                "main.js"   to "e896bab97a748596d52a468168eb1c690a05d4fb71390d9b83b769226db0fb16",
                "bridge.js" to "e02c97f0aa06ba5cdfac32fc10093467d2ccf21241518959770aeeee3d60c651"
            )
            val assetManager = context.assets
            var allOk = true
            for ((file, expected) in expectedHashes) {
                val bytes = assetManager.open("www/$file").readBytes()
                val digest = java.security.MessageDigest.getInstance("SHA-256")
                val hash = digest.digest(bytes).joinToString("") { "%02x".format(it) }
                if (hash != expected) {
                    android.util.Log.w("AutoVPN", "Integrity fail: $file")
                    allOk = false
                }
            }
            allOk
        } catch (_: Exception) { false }
    }

    // -- Open URL --
    // ── Open URL ──────────────────────────────────────────────────────────────



    @JavascriptInterface
    fun getKillSwitch(): Boolean {
        val prefs = context.getSharedPreferences("autovpn", android.content.Context.MODE_PRIVATE)
        return prefs.getBoolean("kill_switch", false)
    }

    @JavascriptInterface
    fun setKillSwitch(enabled: Boolean) {
        val prefs = context.getSharedPreferences("autovpn", android.content.Context.MODE_PRIVATE)
        prefs.edit().putBoolean("kill_switch", enabled).apply()
    }

    @JavascriptInterface
    fun checkAppAllowed(): String {
        return try {
            val url = java.net.URL(
                "https://cdn.jsdelivr.net/gh/artyomandreevich888-bit/autovpn-config@main/allowed.json"
            )
            val conn = url.openConnection() as java.net.HttpURLConnection
            conn.connectTimeout = 5000; conn.readTimeout = 5000
            val body = conn.inputStream.bufferedReader().readText()
            conn.disconnect()
            body
        } catch (_: Exception) {
            """{"allowed":true,"message":""}"""
        }
    }

    @JavascriptInterface
    fun openUrl(url: String) {
        try {
            val intent = Intent(Intent.ACTION_VIEW, Uri.parse(url)).apply {
                addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            }
            context.startActivity(intent)
        } catch (_: Exception) {}
    }
}
