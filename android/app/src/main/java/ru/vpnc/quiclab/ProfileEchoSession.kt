package ru.vpnc.quiclab

import android.content.Context
import android.net.ConnectivityManager
import org.json.JSONObject

/** Authenticated probes only: no Android VPN service, TUN or application routes. */
internal class ProfileEchoSession(c: Context, private val output: (String, JSONObject) -> Unit) {
    private data class Entry(
        val transport: String,
        val endpoint: String,
        val hostname: String,
        val session: VpnSession,
    )

    private val entries = mutableListOf<Entry>()
    @Volatile private var closed = false
    private val ready = mutableMapOf<String, MutableSet<Int>>()
    private val started = mutableSetOf<String>()
    private var requested: Int? = null

    init {
        check(!LabVpnService.active) { "Сначала остановите VPN" }
        val p = VpnProfiles.preferences(c)
        val identity = VpnIdentity.load(c)
        val allowed =
            p.getStringSet("available_transports", setOf("quic", "https", "awg")).orEmpty()
        val transports =
            listOf("quic", "https", "awg").filter {
                it in allowed &&
                    !p.getString("${it}_endpoint", "").isNullOrBlank() &&
                    (if (it == "awg") identity.optString("awg_config").isNotBlank()
                    else identity.optString("key").isNotBlank())
            }
        require(transports.isNotEmpty()) { "Импортируйте VPN-профиль с доступными транспортами" }
        try {
            for (transport in transports) {
                val cfg =
                    JSONObject(identity.toString())
                        .put("transport", transport)
                        .put("ca", p.getString("ca", ""))
                        .put("dns", p.getString("dns", "1.1.1.1"))
                        .put("transit_endpoint", p.getString("transit_endpoint", ""))
                        .put("probe_exit_ip", false)
                val endpoint = p.getString("${transport}_endpoint", "").orEmpty()
                val name = p.getString("hostname", "").orEmpty()
                synchronized(this) { ready[transport] = mutableSetOf() }
                val session =
                    VpnSession(
                        c.getSystemService(ConnectivityManager::class.java),
                        null,
                        cfg,
                        -1,
                        availability = { _, kind, present ->
                            synchronized(this) {
                                if (present) ready[transport]?.add(kind)
                                else ready[transport]?.remove(kind)
                                if (
                                    !closed && present && requested == kind && transport !in started
                                ) {
                                    entries
                                        .firstOrNull { it.transport == transport }
                                        ?.let { start(it, kind) }
                                }
                            }
                        },
                        output = { if (!closed) output(transport, it) },
                        attached = {},
                    )
                synchronized(this) { entries.add(Entry(transport, endpoint, name, session)) }
            }
        } catch (e: Exception) {
            close()
            throw e
        }
    }

    val transports
        get() = entries.map { it.transport }.toSet()

    private fun start(entry: Entry, kind: Int) {
        started.add(entry.transport)
        entry.session.startOrMigrate(kind, entry.endpoint, entry.hostname, "", 100)
    }

    @Synchronized
    fun move(kind: Int) {
        if (closed) return
        requested = kind
        entries.forEach { if (kind in ready[it.transport].orEmpty()) start(it, kind) }
    }

    @Synchronized
    fun close() {
        closed = true
        entries.forEach { it.session.close() }
        entries.clear()
    }
}
