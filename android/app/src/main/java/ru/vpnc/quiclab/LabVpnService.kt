package ru.vpnc.quiclab

import android.app.*
import android.content.Intent
import android.net.ConnectivityManager
import android.net.VpnService
import android.os.Handler
import android.os.Looper
import android.os.ParcelFileDescriptor
import org.json.JSONObject

class LabVpnService : VpnService() {
    private var session: VpnSession? = null
    private var tun: ParcelFileDescriptor? = null
    private val handler = Handler(Looper.getMainLooper())
    private var starting = false

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == "stop") {
            stopSelf()
            return START_NOT_STICKY
        }
        if (session != null) return START_NOT_STICKY
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel("vpn", "QUIC Lab VPN", NotificationManager.IMPORTANCE_LOW)
        )
        val open =
            PendingIntent.getActivity(
                this,
                0,
                Intent(this, VpnActivity::class.java),
                PendingIntent.FLAG_IMMUTABLE,
            )
        val stop =
            PendingIntent.getService(
                this,
                1,
                Intent(this, LabVpnService::class.java).setAction("stop"),
                PendingIntent.FLAG_IMMUTABLE,
            )
        startForeground(
            42,
            Notification.Builder(this, "vpn")
                .setSmallIcon(android.R.drawable.ic_lock_lock)
                .setContentTitle("QUIC Lab VPN")
                .setContentText("Экспериментальный IPv4 туннель")
                .setContentIntent(open)
                .addAction(Notification.Action.Builder(null, "Остановить", stop).build())
                .setOngoing(true)
                .build(),
        )
        try {
            val prefs = getSharedPreferences("vpn", MODE_PRIVATE)
            val mode = prefs.getInt("mode", 0)
            val apps = prefs.getStringSet("apps", emptySet())!!.filter { it != packageName }
            val builder =
                Builder()
                    .setSession("QUIC Lab VPN")
                    .setMtu(1280)
                    .addAddress("10.254.254.1", 32)
                    .setBlocking(true)
                    .setConfigureIntent(open)
            if (mode == 3)
                VpnRoutes.parse(prefs.getString("routes", "")!!).forEach {
                    builder.addRoute(it.first, it.second)
                }
            else {
                builder.addRoute("0.0.0.0", 0)
                builder.addDnsServer(prefs.getString("dns", "1.1.1.1")!!)
            }
            when (mode) {
                1 -> {
                    require(apps.isNotEmpty()) { "Выберите приложения" }
                    apps.forEach { builder.addAllowedApplication(it) }
                }
                2 -> {
                    (apps + packageName).forEach { builder.addDisallowedApplication(it) }
                }
                else -> builder.addDisallowedApplication(packageName)
            }
            // IPv6 is blocked for captured apps; it must not silently bypass this IPv4 VPN.
            tun = builder.establish() ?: error("VPN не разрешён")
            val cfg =
                VpnIdentity.load(this)
                    .put("transport", prefs.getString("transport", "quic"))
                    .put("ca", prefs.getString("ca", ""))
            val endpoint = prefs.getString("endpoint", "")!!
            val hostname = prefs.getString("hostname", "")!!
            val cm = getSystemService(ConnectivityManager::class.java)
            session =
                VpnSession(
                    cm,
                    this,
                    cfg,
                    tun!!.fd,
                    selected = { n, _ -> setUnderlyingNetworks(arrayOf(n)) },
                    availability = { _, kind, up ->
                        if (up)
                            handler.post {
                                if (!starting) {
                                    starting = true
                                    session?.startOrMigrate(kind, endpoint, hostname, "", 100)
                                }
                            }
                    },
                    output = { event ->
                        val type = event.optString("event")
                        if (type == "operation_failed") handler.post { starting = false }
                        record(event)
                    },
                )
            active = true
            status = "Подключаем ${cfg.optString("transport").uppercase()}…"
        } catch (e: Exception) {
            status = "Ошибка: ${e.message}"
            stopSelf()
        }
        return START_NOT_STICKY
    }

    override fun onRevoke() {
        stopSelf()
    }

    override fun onDestroy() {
        session?.close()
        session = null
        tun?.close()
        tun = null
        active = false
        handler.removeCallbacksAndMessages(null)
        if (!status.startsWith("Ошибка")) status = "VPN остановлен"
        super.onDestroy()
    }

    companion object {
        @Volatile var active = false
        @Volatile var status = "VPN выключен"
        @Volatile var rtt = 0.0
        @Volatile var connection = "—"
        private val events = ArrayDeque<String>()

        @Synchronized
        fun record(e: JSONObject) {
            val kind = e.optString("event")
            if (kind == "echo") {
                rtt = e.optDouble("rtt_ms")
                connection = e.optString("connection_id", connection)
                return
            }
            val detail = e.optString("detail", e.optString("error", e.optString("destination", "")))
            events.addLast("$kind  $detail")
            while (events.size > 30) events.removeFirst()
            status =
                when (kind) {
                    "connected" -> "Туннель подключён"
                    "disconnected" -> "Туннель разорван"
                    "active_network" -> "Работает через $detail"
                    "operation_failed" -> "Ошибка: $detail"
                    else -> status
                }
        }

        @Synchronized fun log() = events.joinToString("\n")
    }
}
