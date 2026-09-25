package ru.vpnc.quiclab

import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.ParcelFileDescriptor
import android.os.SystemClock
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import mobile.EventSink
import mobile.Gateway
import mobile.Mobile
import mobile.SocketBinder
import org.json.JSONObject

/** Serializes connection lifecycle and Android network events; never auto-dials. */
internal class VpnSession(
    private val cm: ConnectivityManager,
    private val service: android.net.VpnService,
    private val config: JSONObject,
    private val tunFD: Int,
    private val selected: (Network, Int) -> Unit = { _, _ -> },
    private val availability: (Network, Int, Boolean) -> Unit = { _, _, _ -> },
    private val validation: (Network, Int, Boolean) -> Unit = { _, _, _ -> },
    private val output: (JSONObject) -> Unit,
) {
    private val worker = Executors.newSingleThreadScheduledExecutor()
    private val networks = mutableMapOf<Int, Network>()
    private val validated = mutableSetOf<Network>()
    private val callbacks = mutableListOf<ConnectivityManager.NetworkCallback>()
    private var client: Gateway? = null
    private var alive = false
    private var activeNetwork: Network? = null
    private var activeKind = WIFI
    private var failedNetwork: Network? = null
    private var automatic = true
    private val policy = MigrationPolicy()
    private val readySince = mutableMapOf<Network, Long>()
    private val retryAt = mutableMapOf<Network, Long>()
    private val failures = mutableMapOf<Network, Int>()
    private var preparedNetwork: Network? = null
    private var preparedAt = 0L
    private var nextProbeAt = 0L
    private var nextExitCheckAt = 0L
    private var lastSwitchAt = 0L
    private var lastAttemptAt = 0L
    private var intervalMS = 50L
    @Volatile private var lastEchoAt = 0L
    @Volatile private var smoothedRTT = 100.0
    @Volatile private var standbyRTT = 0.0

    private fun now() = SystemClock.elapsedRealtime()

    private fun key(network: Network) = network.networkHandle.toString()

    @Volatile private var epoch = 0L
    @Volatile private var closed = false

    init {
        watch(WIFI)
        watch(CELLULAR)
        worker.scheduleWithFixedDelay(
            {
                if (!closed)
                    try {
                        tick()
                    } catch (e: Exception) {
                        event("controller_error", e.toString())
                    }
            },
            100,
            100,
            TimeUnit.MILLISECONDS,
        )
    }

    private fun submit(action: () -> Unit) {
        synchronized(worker) { if (!closed) worker.execute { if (!closed) action() } }
    }

    private fun event(kind: String, detail: String) {
        if (!closed) output(JSONObject().put("event", kind).put("detail", detail))
    }

    fun setAutomatic(enabled: Boolean) = submit {
        automatic = enabled
        event(
            "auto_mode",
            if (enabled) "Автомиграция включена; переподключение выключено"
            else "Автомиграция выключена",
        )
        if (enabled && failedNetwork != null) recover("Автомиграция включена")
    }

    fun startOrMigrate(kind: Int, endpoint: String, name: String, pin: String, interval: Long) =
        submit {
            try {
                val network = networks[kind] ?: error("Сеть ${label(kind)} пока недоступна")
                if (alive) {
                    migrate(kind, network, "Нажатие кнопки")
                } else {
                    // A button press explicitly starts a new experiment after a disconnect.
                    stopInternal()
                    val token = epoch
                    val current =
                        Mobile.newGateway(
                            object : EventSink {
                                override fun onEvent(eventJSON: String) {
                                    if (closed || token != epoch) return
                                    val e = JSONObject(eventJSON)
                                    if (e.optString("event") == "echo") {
                                        lastEchoAt = now()
                                        smoothedRTT =
                                            smoothedRTT * 0.875 +
                                                e.optDouble("rtt_ms", 100.0) * 0.125
                                    }
                                    if (e.optString("event") == "standby_ready")
                                        standbyRTT = e.optDouble("probe_ms")
                                    output(e)
                                    if (e.optString("event") == "disconnected")
                                        submit {
                                            if (token == epoch) {
                                                if (config.optString("transport") == "https") {
                                                    failedNetwork = activeNetwork
                                                    retryAt.clear()
                                                    recover("HTTPS соединение закрыто")
                                                } else {
                                                    alive = false
                                                    activeNetwork = null
                                                    failedNetwork = null
                                                }
                                                event(
                                                    "session_closed",
                                                    if (config.optString("transport") == "https")
                                                        "HTTPS переподключается; старые TCP-потоки завершены."
                                                    else
                                                        "QUIC закрыт. Новый запуск — только по кнопке.",
                                                )
                                            }
                                        }
                                    if (e.optString("event") == "path_unavailable")
                                        submit {
                                            if (
                                                token == epoch &&
                                                    alive &&
                                                    e.optString("local") == client?.localAddress()
                                            ) {
                                                if (failedNetwork == null) retryAt.clear()
                                                failedNetwork = activeNetwork
                                                recover("Ошибка сокета текущей сети")
                                            }
                                        }
                                }
                            }
                        )
                    client = current
                    selected(network, kind)
                    val resolved = resolveEndpoint(endpoint, network)
                    config
                        .put("endpoint", resolved.address)
                        .put("hostname", name.ifBlank { resolved.hostname })
                    current.start(config.toString(), binder(network))
                    current.attach(tunFD.toLong())
                    nextExitCheckAt = 0L
                    alive = true
                    intervalMS = interval
                    lastEchoAt = now()
                    lastSwitchAt = now()
                    smoothedRTT = 100.0
                    activeKind = kind
                    activeNetwork = network
                    event("active_network", label(kind))
                }
            } catch (e: Exception) {
                event("operation_failed", e.message ?: e.toString())
            }
        }

    private fun migrate(kind: Int, network: Network, reason: String) {
        val current = client ?: return
        if (network == activeNetwork && failedNetwork == null) {
            event("active_network", label(kind))
            return
        }
        event("migration_started", "$reason → ${label(kind)}")
        current.migrateTo(key(network), binder(network))
        preparedNetwork = null
        preparedAt = 0
        lastSwitchAt = now()
        lastEchoAt = now()
        nextProbeAt = now() + 500
        activeKind = kind
        activeNetwork = network
        failedNetwork = null
        selected(network, kind)
        event("active_network", label(kind))
    }

    private fun recover(reason: String) {
        if (!automatic || !alive || now() - lastAttemptAt < 750) return
        lastAttemptAt = now()
        val previous = activeNetwork
        val candidate =
            listOf(WIFI, CELLULAR)
                .mapNotNull { kind ->
                    networks[kind]
                        ?.takeIf { it != activeNetwork && (retryAt[it] ?: 0L) <= now() }
                        ?.let { kind to it }
                }
                .firstOrNull()
                ?: if (config.optString("transport") == "https" && failedNetwork != null)
                    networks[activeKind]
                        ?.takeIf { (retryAt[it] ?: 0L) <= now() }
                        ?.let { activeKind to it }
                else null
        if (candidate == null) {
            if (failedNetwork != previous)
                event("waiting_network", "$reason. Ожидание доступного пути без нового соединения.")
            failedNetwork = previous
            return
        }
        try {
            migrate(candidate.first, candidate.second, reason)
            previous?.let { penalize(it) }
        } catch (e: Exception) {
            failedNetwork = previous
            penalize(candidate.second)
            event("auto_migration_failed", e.message ?: e.toString())
        }
    }

    private fun penalize(network: Network) {
        val count = (failures[network] ?: 0) + 1
        failures[network] = count.coerceAtMost(5)
        retryAt[network] = now() + policy.retryDelay(count)
        readySince.remove(network)
        if (preparedNetwork == network) preparedNetwork = null
    }

    fun checkExitIP() = submit {
        if (alive && config.optBoolean("probe_exit_ip")) {
            client?.checkExitIP()
            nextExitCheckAt = now() + 60000
        }
    }

    private fun tick() {
        if (!alive) return
        val time = now()
        if (config.optBoolean("probe_exit_ip") && time >= nextExitCheckAt && time - lastEchoAt < 2000) {
            client?.checkExitIP()
            nextExitCheckAt = time + 60000
        }
        if (!automatic) return
        if (
            activeNetwork in networks.values &&
                lastEchoAt > lastAttemptAt &&
                time - lastEchoAt < maxOf(150L, intervalMS * 2)
        ) {
            failedNetwork = null
            if (time - lastSwitchAt > 30000)
                activeNetwork?.let {
                    failures.remove(it)
                    retryAt.remove(it)
                }
        }
        if (policy.isStalled(time - lastEchoAt, smoothedRTT, intervalMS)) {
            recover("Нет ответов сервера: ${time - lastEchoAt} мс")
        } else if (failedNetwork != null) {
            recover("Текущий путь недоступен")
        }
        if (!alive || time < nextProbeAt) return
        val reserve =
            networks.entries.firstOrNull {
                it.value != activeNetwork && (retryAt[it.value] ?: 0L) <= time
            } ?: return
        nextProbeAt = time + 1500
        try {
            client?.preparePath(key(reserve.value), binder(reserve.value)) ?: return
            preparedNetwork = reserve.value
            preparedAt = now()
            readySince.putIfAbsent(reserve.value, now())
            if (
                activeKind == CELLULAR &&
                    reserve.key == WIFI &&
                    reserve.value in validated &&
                    policy.canPreferWifi(
                        now() - (readySince[reserve.value] ?: now()),
                        now() - lastSwitchAt,
                    ) &&
                    standbyRTT < maxOf(150.0, smoothedRTT * 1.25)
            ) {
                migrate(WIFI, reserve.value, "Wi-Fi устойчиво отвечает")
            } else if (
                smoothedRTT > 600.0 &&
                    standbyRTT * 2 < smoothedRTT &&
                    now() - lastSwitchAt > 8000 &&
                    now() - (readySince[reserve.value] ?: now()) > 3000
            ) {
                recover("У резервного пути устойчиво меньше задержка")
            }
        } catch (e: Exception) {
            preparedNetwork = null
            penalize(reserve.value)
            event("standby_unavailable", "${label(reserve.key)}: ${e.message}")
        }
    }

    private fun watch(kind: Int) {
        val callback =
            object : ConnectivityManager.NetworkCallback() {
                override fun onAvailable(network: Network) = submit {
                    networks[kind] = network
                    availability(network, kind, true)
                    event("network_available", "${label(kind)} $network")
                }

                override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) =
                    submit {
                        val ready = caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
                        validation(network, kind, ready)
                        val newlyReady =
                            if (ready) validated.add(network)
                            else {
                                validated.remove(network)
                                false
                            }
                        if (newlyReady) event("network_validated", label(kind))
                        if (alive && newlyReady) nextProbeAt = 0
                    }

                override fun onLosing(network: Network, maxMsToLive: Int) = submit {
                    if (
                        network == activeNetwork &&
                            preparedNetwork != null &&
                            now() - preparedAt < 3000
                    ) {
                        recover("Android сообщает о скорой потере сети")
                    }
                }

                override fun onLost(network: Network) = submit {
                    if (networks[kind] == network) networks.remove(kind)
                    availability(network, kind, false)
                    validated.remove(network)
                    readySince.remove(network)
                    retryAt.remove(network)
                    failures.remove(network)
                    if (preparedNetwork == network) preparedNetwork = null
                    client?.invalidatePath(key(network))
                    event("network_lost", "${label(kind)} $network")
                    if (activeNetwork == network) {
                        retryAt.clear()
                        failedNetwork = network
                        recover("Текущая сеть потеряна")
                    }
                }

                override fun onUnavailable() = submit { event("network_unavailable", label(kind)) }
            }
        try {
            cm.requestNetwork(
                NetworkRequest.Builder()
                    .addTransportType(kind)
                    .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
                    .build(),
                callback,
            )
            callbacks.add(callback)
        } catch (e: Exception) {
            event("network_request_failed", e.message ?: e.toString())
        }
    }

    fun stop() = submit {
        stopInternal()
        event("stopped", "Опыт остановлен")
    }

    private fun stopInternal() {
        epoch++
        alive = false
        activeNetwork = null
        failedNetwork = null
        preparedNetwork = null
        readySince.clear()
        retryAt.clear()
        failures.clear()
        lastAttemptAt = 0
        nextProbeAt = 0
        client?.stop()
        client = null
    }

    fun close() {
        synchronized(worker) {
            if (closed) return
            closed = true
            callbacks.forEach { callback ->
                try { cm.unregisterNetworkCallback(callback) } catch (_: IllegalArgumentException) { }
            }
            worker.execute { stopInternal() }
            worker.shutdown()
        }
    }

    private fun binder(network: Network) =
        object : SocketBinder {
            override fun bind(fd: Long) {
                check(service.protect(fd.toInt())) { "Cannot protect tunnel socket" }
                ParcelFileDescriptor.fromFd(fd.toInt()).use {
                    network.bindSocket(it.fileDescriptor)
                }
            }
        }

    companion object {
        const val WIFI = NetworkCapabilities.TRANSPORT_WIFI
        const val CELLULAR = NetworkCapabilities.TRANSPORT_CELLULAR

        fun label(kind: Int) = if (kind == WIFI) "Wi-Fi" else "LTE/Cellular"
    }
}
