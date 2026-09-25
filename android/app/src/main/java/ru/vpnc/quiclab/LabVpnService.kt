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
        Diagnostics.init(applicationContext)
        if (intent?.action == "exit-ip") {
            if (active && exitEnabled) session?.checkExitIP()
            return START_NOT_STICKY
        }
        if (intent?.action == "stop") {
            Diagnostics.event("vpn", JSONObject().put("event", "stop_requested"))
            shutdown()
            stopSelf()
            return START_NOT_STICKY
        }
        if (intent?.action == "move") {
            if (active) {
                val p=VpnProfiles.preferences(this)
                session?.startOrMigrate(intent.getIntExtra("network",VpnSession.WIFI),p.getString("endpoint","")!!,p.getString("hostname","")!!,"",100)
            }
            return START_NOT_STICKY
        }
        if (session != null) return START_NOT_STICKY
        resetMetrics(VpnProfiles.preferences(this).getString("transport","quic")!!)
        connection = "—"
        rtt = 0.0
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
            val prefs = VpnProfiles.preferences(this)
            val mode = prefs.getInt("mode", 0)
            exitEnabled = mode != 3
            exitState = if (exitEnabled) "Ожидаем туннель" else "Не проверяется в режиме подсетей"
            val apps = VpnProfiles.apps(this).filter { it != packageName }
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
                    .put("probe_exit_ip", mode != 3)
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
                                if (active && !starting) {
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
            Diagnostics.event("vpn", JSONObject().put("event","start_failed").put("error",e.toString()))
            shutdown()
            stopSelf()
        }
        return START_NOT_STICKY
    }

    override fun onRevoke() {
        Diagnostics.event("vpn", JSONObject().put("event","permission_revoked"))
        shutdown()
        stopSelf()
    }

    // Android may keep VpnService bound after stopSelf; release the VPN explicitly.
    private fun shutdown() {
        if (active) Diagnostics.event("vpn", JSONObject().put("event","stopped"))
        active = false
        exitState = "VPN остановлен"
        handler.removeCallbacksAndMessages(null)
        session?.close()
        session = null
        tun?.close()
        tun = null
        starting = false
        txRate=0.0; rxRate=0.0
        connection = "—"
        rtt = 0.0
        stopForeground(STOP_FOREGROUND_REMOVE)
        if (!status.startsWith("Ошибка")) status = "VPN остановлен"
    }

    override fun onDestroy() {
        shutdown()
        super.onDestroy()
    }

    companion object {
        @Volatile var exitEnabled = true
        @Volatile var exitIP = ""
        @Volatile var exitState = "Не проверен"
        @Volatile var exitCheckedAt = 0L
        @Volatile var active = false
        @Volatile var status = "VPN выключен"
        @Volatile var rtt = 0.0
        @Volatile var connection = "—"
        @Volatile var transport = "quic"
        @Volatile var startedAt = 0L
        @Volatile var lastEcho = 0L
        @Volatile var network = "—"
        @Volatile var txBytes = 0L
        @Volatile var rxBytes = 0L
        @Volatile var txRate = 0.0
        @Volatile var rxRate = 0.0
        @Volatile var lastTransition = 0L
        private var trafficAt = 0L
        private var stats = TransportStats()
        private val events = ArrayDeque<String>()
        @Synchronized private fun resetMetrics(value:String) {
            exitIP=""; exitCheckedAt=0; exitState="Не проверен"
            transport=value; startedAt=android.os.SystemClock.elapsedRealtime()
            lastEcho=0; network="—"; txBytes=0; rxBytes=0; txRate=0.0; rxRate=0.0
            lastTransition=0; trafficAt=startedAt; stats=TransportStats(); events.clear()
        }
        @Synchronized fun quality(now:Long):String {
            val jitter=stats.maxJitter(now)?.let{"%.1f мс".format(it)} ?: "—"
            val gap=if(lastEcho==0L) "—" else "%.0f мс".format(maxOf(stats.maxGap,if(active)(now-lastEcho).toDouble() else 0.0))
            return "Jitter max · 15 с: $jitter   ·   Пауза макс.: $gap\nСеансов: ${stats.sessions.size}"
        }

        @Synchronized
        fun record(e: JSONObject) {
            Diagnostics.event("vpn", e)
            val kind = e.optString("event")
            when(kind) {
                "exit_ip_checking" -> { exitState="Проверяем…"; return }
                "exit_ip" -> { exitIP=e.optString("ip"); exitCheckedAt=android.os.SystemClock.elapsedRealtime(); exitState="Проверен через туннель"; return }
                "exit_ip_failed" -> { exitState="Проверка недоступна"; return }
            }
            val now=android.os.SystemClock.elapsedRealtime()
            if(kind=="traffic") {
                val tx=e.optLong("tx_bytes"); val rx=e.optLong("rx_bytes")
                val seconds=(now-trafficAt).coerceAtLeast(1)/1000.0
                txRate=(tx-txBytes).coerceAtLeast(0)/seconds
                rxRate=(rx-rxBytes).coerceAtLeast(0)/seconds
                txBytes=tx; rxBytes=rx; trafficAt=now
                return
            }
            if (kind=="standby_ready") return
            stats.accept(e,now)
            if(kind=="active_network") network=e.optString("detail")
            if(kind in listOf("active_network","path_switched","network_lost")) lastTransition=now
            if (kind == "echo") {
                lastEcho=now
                rtt = e.optDouble("rtt_ms")
                connection = e.optString("connection_id", connection)
                return
            }
            val detail = e.optString("detail", e.optString("error", e.optString("destination", "")))
            events.addLast("${java.time.Instant.now()}  $kind  $detail")
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
