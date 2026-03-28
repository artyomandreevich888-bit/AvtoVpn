package com.autovpn

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Intent
import android.net.VpnService
import android.os.ParcelFileDescriptor
import mobile.Mobile
import mobile.StatusListener
import mobile.VPNService as GoVPNService

class AutoVpnService : VpnService() {

    companion object {
        const val ACTION_START = "com.autovpn.START"
        const val ACTION_STOP = "com.autovpn.STOP"
        const val CHANNEL_ID = "autovpn_status"
        const val NOTIFICATION_ID = 1
        var instance: AutoVpnService? = null
    }

    private var tunFd: ParcelFileDescriptor? = null

    override fun onCreate() {
        super.onCreate()
        instance = this
        createNotificationChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                disconnect()
                return START_NOT_STICKY
            }
            ACTION_START -> {
                connect()
                return START_STICKY
            }
        }
        return START_NOT_STICKY
    }

    private fun connect() {
        startForeground(NOTIFICATION_ID, buildNotification("Connecting..."))

        val cacheDir = cacheDir.absolutePath
        val service = this

        Thread {
            try {
                val statusCb = object : StatusListener {
                    override fun onStatusChanged(
                        state: Long, server: String, delayMs: Long,
                        aliveCount: Long, totalCount: Long, errorMsg: String
                    ) {
                        when (state) {
                            Mobile.StateConnected -> {
                                val text = if (server.isNotEmpty()) "$server (${delayMs}ms)" else "Connected"
                                updateNotification(text)
                            }
                            Mobile.StateError -> {
                                updateNotification("Error: $errorMsg")
                                disconnect()
                            }
                        }
                        MainActivity.instance?.runOnUiThread {
                            MainActivity.instance?.updateUI(state, server, delayMs, aliveCount, totalCount, errorMsg)
                        }
                    }
                }

                val vpnBridge = object : GoVPNService {
                    override fun protect(fd: Int): Boolean {
                        return service.protect(fd)
                    }
                }

                // Detect default network interface BEFORE TUN (while network is normal)
                val cm = getSystemService(android.net.ConnectivityManager::class.java)
                val activeNet = cm.activeNetwork
                val lp = if (activeNet != null) cm.getLinkProperties(activeNet) else null
                val netIfName = lp?.interfaceName ?: "wlan0"
                val netIfIndex = try {
                    java.net.NetworkInterface.getByName(netIfName)?.index ?: 0
                } catch (_: Exception) { 0 }
                android.util.Log.i("AutoVPN", "Default interface: $netIfName idx=$netIfIndex")

                // Step 1: Fetch configs BEFORE creating TUN (network still open)
                val configJSON = Mobile.prepare(cacheDir, statusCb)

                // Step 2: Now create TUN — all traffic will be captured
                val fd = Builder()
                    .setSession("AutoVPN")
                    .addAddress("172.19.0.1", 30)
                    .addRoute("0.0.0.0", 0)
                    .addRoute("::", 0)
                    .addDnsServer("8.8.8.8")
                    .addDnsServer("1.1.1.1")
                    .setMtu(9000)
                    .establish()

                if (fd == null) {
                    android.util.Log.e("AutoVPN", "Failed to establish TUN")
                    disconnect()
                    return@Thread
                }

                tunFd = fd

                // Step 3: Start sing-box with TUN FD and pre-fetched config
                Mobile.start(fd.fd, configJSON, netIfName, netIfIndex, vpnBridge, statusCb)
            } catch (e: Exception) {
                android.util.Log.e("AutoVPN", "Start failed", e)
                disconnect()
            }
        }.start()
    }

    private fun disconnect() {
        Thread {
            try { Mobile.stop() } catch (_: Exception) {}
        }.start()
        tunFd?.close()
        tunFd = null
        stopForeground(STOP_FOREGROUND_REMOVE)
        stopSelf()
        instance = null
        MainActivity.instance?.runOnUiThread {
            MainActivity.instance?.updateDisconnected()
        }
    }

    override fun onDestroy() {
        disconnect()
        super.onDestroy()
    }

    private fun createNotificationChannel() {
        val channel = NotificationChannel(
            CHANNEL_ID, "VPN Status", NotificationManager.IMPORTANCE_LOW
        )
        getSystemService(NotificationManager::class.java).createNotificationChannel(channel)
    }

    private fun buildNotification(text: String): Notification {
        val stopIntent = Intent(this, AutoVpnService::class.java).apply {
            action = ACTION_STOP
        }
        val stopPending = PendingIntent.getService(
            this, 0, stopIntent, PendingIntent.FLAG_IMMUTABLE
        )

        return Notification.Builder(this, CHANNEL_ID)
            .setContentTitle("AutoVPN")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_lock_lock)
            .addAction(
                Notification.Action.Builder(null, "Disconnect", stopPending).build()
            )
            .setOngoing(true)
            .build()
    }

    private fun updateNotification(text: String) {
        getSystemService(NotificationManager::class.java)
            .notify(NOTIFICATION_ID, buildNotification(text))
    }
}
