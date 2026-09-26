package ru.vpnc.quiclab

import android.net.ConnectivityManager
import android.net.Network
import android.net.VpnService
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import java.net.InetSocketAddress
import mobile.*
import org.json.JSONObject

internal class MultipleVpnController(
    private val service: VpnService,
    private val plan: MultipleVpnPlan,
    fd: Int,
) {
    private val cm = service.getSystemService(ConnectivityManager::class.java)
    private val handler = Handler(Looper.getMainLooper())
    private val sessions = mutableMapOf<String, VpnSession>()
    private val networks = mutableMapOf<String, Network>()
    private val starting = mutableSetOf<String>()
    private val paused = mutableSetOf<String>()
    @Volatile private var closed = false
    private val generations = java.util.concurrent.ConcurrentHashMap<String, Long>()
    private val available = mutableMapOf<String, MutableSet<Int>>()
    private var generation = 0L
    private val router =
        Mobile.newMultiRouter(
            plan.rules,
            plan.dnsProfile,
            object : FlowOwner {
                override fun owner(
                    protocol: Long,
                    local: String,
                    localPort: Long,
                    remote: String,
                    remotePort: Long,
                ): Long =
                    try {
                        cm.getConnectionOwnerUid(
                                protocol.toInt(),
                                InetSocketAddress(local, localPort.toInt()),
                                InetSocketAddress(remote, remotePort.toInt()),
                            )
                            .toLong()
                    } catch (e: Exception) {
                        -1L
                    }
            },
            object : SocketBinder {
                override fun bind(fd: Long) {
                    check(service.protect(fd.toInt())) { "Cannot protect direct socket" }
                }
            },
            object : EventSink {
                override fun onEvent(eventJSON: String) {
                    if (closed) return
                    val e = JSONObject(eventJSON)
                    MultipleVpnState.record(e.optString("profile_id"), e)
                    Diagnostics.event("multiple", e)
                }
            },
        )

    init {
        MultipleVpnState.reset(plan.profiles)
        try {
            router.attach(fd.toLong())
            plan.profiles.forEach { start(it) }
        } catch (e: Exception) {
            close()
            throw e
        }
        handler.postDelayed(
            object : Runnable {
                override fun run() {
                    if (closed) return
                    plan.profiles
                        .filter { it.id !in paused && it.id !in starting }
                        .forEach { p ->
                            val kinds = available[p.id].orEmpty()
                            val kind =
                                if (VpnSession.WIFI in kinds) VpnSession.WIFI
                                else kinds.firstOrNull()
                            if (kind != null) {
                                starting.add(p.id)
                                sessions[p.id]?.startOrMigrate(
                                    kind,
                                    p.endpoint,
                                    p.hostname,
                                    "",
                                    100,
                                )
                            }
                        }
                    handler.postDelayed(this, 5000)
                }
            },
            5000,
        )
    }

    private fun start(p: MultipleProfile) {
        val token = ++generation
        generations[p.id] = token
        available[p.id] = mutableSetOf()
        val session =
            VpnSession(
                cm,
                service,
                JSONObject(p.config.toString()),
                -1,
                selected = { n, _ ->
                    handler.post {
                        if (!closed && generations[p.id] == token) {
                            networks[p.id] = n
                            service.setUnderlyingNetworks(networks.values.distinct().toTypedArray())
                        }
                    }
                },
                availability = { _, kind, up ->
                    handler.post {
                        if (!closed && generations[p.id] == token) {
                            val kinds = available.getOrPut(p.id) { mutableSetOf() }
                            if (up) kinds.add(kind) else kinds.remove(kind)
                            if (up && p.id !in paused && starting.add(p.id))
                                sessions[p.id]?.startOrMigrate(
                                    kind,
                                    p.endpoint,
                                    p.hostname,
                                    "",
                                    100,
                                )
                        }
                    }
                },
                output = { event ->
                    handler.post {
                        if (!closed && generations[p.id] == token) {
                            MultipleVpnState.record(p.id, event)
                            Diagnostics.event(
                                "multiple",
                                JSONObject(event.toString()).put("profile_id", p.id),
                            )
                            if (
                                event.optString("event") in
                                    listOf("operation_failed", "session_closed")
                            )
                                starting.remove(p.id)
                        }
                    }
                },
                attached = { g ->
                    check(!closed && generations[p.id] == token) { "Profile stopped" }
                    router.setGateway(p.id, g)
                },
                detached = { g -> router.detachGateway(p.id, g) },
            )
        sessions[p.id] = session
    }

    fun toggle(id: String) {
        if (closed) return
        val p = plan.profiles.firstOrNull { it.id == id } ?: return
        if (paused.remove(id)) {
            router.setProfileEnabled(id, true)
            starting.remove(id)
            start(p)
        } else {
            paused.add(id)
            router.setProfileEnabled(id, false)
            generations[id] = ++generation
            available.remove(id)
            starting.remove(id)
            sessions.remove(id)?.close()
            networks.remove(id)
            service.setUnderlyingNetworks(networks.values.distinct().toTypedArray())
            MultipleVpnState.record(id, JSONObject().put("event", "stopped"))
        }
    }

    fun move(kind: Int) {
        plan.profiles
            .filter { it.id !in paused }
            .forEach { sessions[it.id]?.startOrMigrate(kind, it.endpoint, it.hostname, "", 100) }
    }

    fun close() {
        if (closed) return
        closed = true
        plan.profiles.forEach {
            MultipleVpnState.record(it.id, JSONObject().put("event", "stopped"))
        }
        handler.removeCallbacksAndMessages(null)
        sessions.values.forEach { it.close() }
        sessions.clear()
        // Cancellation can wait for active flows; never block Android's service/main thread.
        Thread({ router.close() }, "multiple-close").start()
    }
}

internal object MultipleVpnState {
    private data class State(
        val name: String,
        val transport: String,
        var status: String = "Подключаем…",
        var network: String = "—",
        var rtt: Double = 0.0,
        var echo: Long = 0,
        var tx: Long = 0,
        var rx: Long = 0,
        var at: Long = 0,
        var txRate: Double = 0.0,
        var rxRate: Double = 0.0,
    )

    private val states = linkedMapOf<String, State>()
    private var blocked = 0L

    @Synchronized
    fun reset(ps: List<MultipleProfile>) {
        states.clear()
        blocked = 0
        ps.forEach { states[it.id] = State(it.name, it.config.optString("transport").uppercase()) }
    }

    @Synchronized
    fun record(id: String, e: JSONObject) {
        if (e.optString("event") == "route_blocked") {
            blocked++
            return
        }
        val s = states[id] ?: return
        val now = SystemClock.elapsedRealtime()
        when (e.optString("event")) {
            "active_network" -> s.network = e.optString("detail")
            "echo" -> {
                s.rtt = e.optDouble("rtt_ms")
                s.echo = now
                s.status = "Работает"
            }
            "connected" -> {
                s.status = "Подключён, ждём ответ"
                s.echo = 0
            }
            "disconnected",
            "session_closed",
            "operation_failed" -> {
                s.status = "Нет связи"
                s.echo = 0
            }
            "stopped" -> {
                s.status = "Остановлен · маршрут заблокирован"
                s.echo = 0
                s.txRate = 0.0
                s.rxRate = 0.0
            }
            "profile_traffic" -> {
                val tx = e.optLong("tx_bytes")
                val rx = e.optLong("rx_bytes")
                val dt = (now - s.at) / 1000.0
                if (s.at > 0 && dt > 0) {
                    s.txRate = maxOf(0L, tx - s.tx) / dt
                    s.rxRate = maxOf(0L, rx - s.rx) / dt
                }
                s.tx = tx
                s.rx = rx
                s.at = now
            }
        }
    }

    @Synchronized
    fun notification(now: Long): VpnNotificationContent {
        val channels = states.values.map { "${vpnNetworkLabel(it.network)} · ${it.transport}" }.distinct().joinToString(" / ")
        val text = states.values.joinToString("\n\n") {
            val fresh = it.echo > 0 && now - it.echo < 3500
            val rateFresh = it.at > 0 && now - it.at < 3500
            val latency = if(fresh) "RTT %.0f мс".format(it.rtt) else "RTT —"
            val status = if(it.echo > 0 && !fresh) "Нет свежих ответов" else it.status
            "${it.name} · ${vpnNetworkLabel(it.network)} · ${it.transport}\n$status · $latency\n" +
                vpnTrafficRate(if(rateFresh) it.txRate else 0.0, if(rateFresh) it.rxRate else 0.0)
        }
        return VpnNotificationContent("VPN · ${states.size} профилей", channels, text)
    }

    @Synchronized
    fun summary(): String {
        val now = SystemClock.elapsedRealtime()
        return states.values.joinToString("\n\n") {
            val fresh = it.echo > 0 && now - it.echo < 3500
            val rateFresh = it.at > 0 && now - it.at < 3500
            "${it.name} · ${it.transport}\n${if(it.echo>0&&!fresh) "Нет свежих ответов" else it.status} · RTT ${if(fresh) "%.0f мс".format(it.rtt) else "—"}\n↑ %.1f KiB/с · ↓ %.1f KiB/с"
                .format(
                    if (rateFresh) it.txRate / 1024 else 0.0,
                    if (rateFresh) it.rxRate / 1024 else 0.0,
                ) + "\nВсего ↑ %.1f KiB · ↓ %.1f KiB".format(it.tx / 1024.0, it.rx / 1024.0)
        } + if (blocked > 0) "\n\nЗаблокировано потоков: $blocked · причины в диагностике" else ""
    }
}
