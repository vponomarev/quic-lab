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

/** Serializes connection lifecycle and Android network events; recovers VPN transports without recreating the Android TUN. */
internal class VpnSession(
    private val cm: ConnectivityManager,
    private val service: android.net.VpnService?,
    private val config: JSONObject,
    private val tunFD: Int,
    private val budget: mobile.TrafficBudget? = null,
    private val selected: (Network, Int) -> Unit = { _, _ -> },
    private val availability: (Network, Int, Boolean) -> Unit = { _, _, _ -> },
    private val validation: (Network, Int, Boolean) -> Unit = { _, _, _ -> },
    private val output: (JSONObject) -> Unit,
    private val attached: ((Gateway) -> Unit)? = null,
    private val detached: ((Gateway) -> Unit)? = null,
) : AutoCloseable {
    private val isBond = config.optBoolean("max_availability")
    private val bondNetworks = mutableMapOf<Int, Network>()
    private val bondRetry = mutableMapOf<Int, Long>()
    private fun refreshBondPaths() {
        if (!alive || !isBond) return
        service?.setUnderlyingNetworks(networks.values.toTypedArray())
        for ((kind, network) in networks) {
            val name = if (kind == WIFI) "wifi" else "cell"
            if (kind == CELLULAR && (!cellAllowed() || client?.bondCellAllowed() == false)) {
                client?.dropBondPath(name); bondNetworks.remove(kind); continue
            }
            if ((bondRetry[kind] ?: 0L) > now()) continue
            bondRetry[kind] = now() + 5000
            try {
                if (bondNetworks[kind] != null && bondNetworks[kind] != network) client?.dropBondPath(name)
                client?.ensureBondPath(name, binder(network))
                if (bondNetworks[kind] != network) event("bond_path", "${label(kind)}: канал подключён")
                bondNetworks[kind] = network
            } catch (e: Exception) { event("bond_path_unavailable", "${label(kind)}: ${e.message}") }
        }
    }
    private val isAWG = config.optString("transport") == "awg"
    private val worker = Executors.newSingleThreadScheduledExecutor()
    private val networks = mutableMapOf<Int, Network>()
    private val validated = mutableSetOf<Network>()
    private val callbacks = mutableListOf<ConnectivityManager.NetworkCallback>()
    private val unmetered = mutableSetOf<Network>()
    private var cellRequest: ConnectivityManager.NetworkCallback? = null
    private var manualCellUntil = 0L
    private var pendingCellStart: (() -> Unit)? = null
    private val reserveListener = android.content.SharedPreferences.OnSharedPreferenceChangeListener { _, _ -> submit { refreshReserve() } }
    private fun reserveAllowed(kind: Int, network: Network? = networks[kind]): Boolean =
        service?.let { VpnReserveSettings.allowed(it, kind, network in unmetered) } ?: true

    private fun cellAllowed() = budget?.cellAllowed() != false
    private var budgetReported = false
    private fun candidateAllowed(kind: Int, network: Network) =
        (kind != CELLULAR || cellAllowed()) &&
        policy.canProbeCandidate(kind == WIFI, alive && activeKind == CELLULAR, reserveAllowed(kind, network))

    private fun refreshReserve() {
        if (closed) return
        networks.forEach { (kind, network) ->
            if (!isBond && network != activeNetwork && !candidateAllowed(kind, network)) {
                client?.invalidatePath(key(network))
                readySince.remove(network)
                if (preparedNetwork == network) preparedNetwork = null
            }
        }
        // A failed primary may acquire mobile data for recovery, even when prewarming is disabled.
        val needCell = cellAllowed() && (if (isBond) alive && client?.bondCellAllowed() != false else (alive && (activeKind == CELLULAR || failedNetwork != null ||
            (automatic && reserveAllowed(CELLULAR)))) || now() < manualCellUntil)
        if (needCell && cellRequest == null) {
            val callback = object : ConnectivityManager.NetworkCallback() {}
            try {
                cm.requestNetwork(NetworkRequest.Builder().addTransportType(CELLULAR)
                    .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET).build(), callback)
                cellRequest = callback
            } catch (e: Exception) { event("network_request_failed", e.toString()) }
        } else if (!needCell) {
            cellRequest?.let { try { cm.unregisterNetworkCallback(it) } catch (_: IllegalArgumentException) {} }
            cellRequest = null
        }
    }
    private var client: Gateway? = null
    private var alive = false
    private var reconnectRequired = false
    @Volatile private var lastReplyAt = 0L
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
    private var rttMode = -1L
    private val rttReceiver = object : android.content.BroadcastReceiver() {
        override fun onReceive(context: android.content.Context?, intent: android.content.Intent?) { submit { refreshRTT(); refreshReserve() } }
    }
    private val rttListener = android.content.SharedPreferences.OnSharedPreferenceChangeListener { _, _ -> submit { refreshRTT() } }
    private fun refreshRTT() {
        val context = service ?: return // Echo retains its independent cadence.
        val mode = VpnRttSettings.interval(context)
        if (mode == rttMode) return
        rttMode = mode
        intervalMS = if (mode == 0L) 5000 else mode
        client?.setRTT(mode > 0, intervalMS)
        // Allow the first scheduled health reply before evaluating a stall.
        lastEchoAt = now(); lastReplyAt = now()
        output(JSONObject().put("event", "rtt_policy").put("enabled", mode > 0).put("interval_ms", intervalMS))
    }
    @Volatile private var awgPingSeen = false
    @Volatile private var lastEchoAt = 0L
    @Volatile private var smoothedRTT = 100.0
    @Volatile private var standbyRTT = 0.0

    private fun now() = SystemClock.elapsedRealtime()

    private fun key(network: Network) = network.networkHandle.toString()

    @Volatile private var epoch = 0L
    @Volatile private var closed = false

    init {
        require(service != null || (tunFD == -1 && attached != null)) { "Probe sessions must not attach a TUN" }
        service?.let {
            androidx.core.content.ContextCompat.registerReceiver(it, rttReceiver,
                android.content.IntentFilter().apply { addAction(android.content.Intent.ACTION_SCREEN_ON); addAction(android.content.Intent.ACTION_SCREEN_OFF) },
                androidx.core.content.ContextCompat.RECEIVER_NOT_EXPORTED)
            VpnRttSettings.preferences(it).registerOnSharedPreferenceChangeListener(rttListener)
            VpnReserveSettings.preferences(it).registerOnSharedPreferenceChangeListener(reserveListener)
        }
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
        refreshReserve()
        event(
            "auto_mode",
            if (enabled) "Автомиграция и восстановление VPN включены"
            else "Автомиграция выключена",
        )
        if (enabled && failedNetwork != null) recover("Автомиграция включена")
    }

    fun startOrMigrate(kind: Int, endpoint: String, name: String, pin: String, interval: Long): Unit =
        submit {
            try {
                if (kind == CELLULAR) { check(cellAllowed()) { "Общий лимит LTE исчерпан; доступен только Wi-Fi" }; manualCellUntil = now() + 15000; refreshReserve() }
                val network = networks[kind] ?: run {
                    if (kind == CELLULAR) {
                        pendingCellStart = { startOrMigrate(kind, endpoint, name, pin, interval) }
                        event("waiting_network", "Ожидаем мобильную сеть для ручного переключения")
                        return@submit
                    }
                    error("Сеть ${label(kind)} пока недоступна")
                }
                if (alive && isBond) {
                    refreshBondPaths()
                    event("bond_policy", "Два пути управляются автоматически; приоритет у Wi-Fi")
                    return@submit
                }
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
                                    if (e.optString("event") in listOf("echo", "health")) {
                                        if (isAWG) awgPingSeen = true
                                        lastEchoAt = now()
                                        lastReplyAt = lastEchoAt
                                        if (e.has("rtt_ms")) smoothedRTT =
                                            smoothedRTT * 0.875 +
                                                e.optDouble("rtt_ms", 100.0) * 0.125
                                    }
                                    if (e.optString("event") == "standby_ready")
                                        standbyRTT = e.optDouble("probe_ms")
                                    output(e)
                                    if (e.optString("event") == "disconnected")
                                        submit {
                                            if (token == epoch) {
                                                if (!isAWG && client?.isConnected() != true) {
                                                    reconnectRequired = true
                                                    failedNetwork = activeNetwork
                                                    retryAt.clear()
                                                    event("reconnecting", "Транспорт закрыт. Восстанавливаем VPN; старые TCP-потоки завершены.")
                                                    recover("Соединение закрыто")
                                                }
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
                    rttMode = -1L
                    refreshRTT()
                    selected(network, kind)
                    val resolved = resolveEndpoint(endpoint, network)
                    config
                        .put("endpoint", resolved.address)
                        .put("hostname", name.ifBlank { resolved.hostname })
                    config.put("initial_path", if (kind == WIFI) "wifi" else "cell")
                    current.start(config.toString(), binder(network))
                    if (isBond) bondNetworks[kind] = network
                    if (attached != null) attached.invoke(current) else current.attach(tunFD.toLong())
                    nextExitCheckAt = 0L
                    alive = true
                    reconnectRequired = false
                    lastReplyAt = now()
                    if (service == null) intervalMS = if (isAWG) 1000 else interval
                    lastEchoAt = now()
                    lastSwitchAt = now()
                    smoothedRTT = 100.0
                    activeKind = kind
                    activeNetwork = network
                    refreshReserve()
                    event("active_network", label(kind))
                }
            } catch (e: Exception) {
                event("operation_failed", e.message ?: e.toString())
            }
        }

    private fun migrate(kind: Int, network: Network, reason: String) {
        val current = client ?: return
        if (network == activeNetwork && failedNetwork == null && !reconnectRequired) {
            event("active_network", label(kind))
            return
        }
        event("migration_started", "$reason → ${label(kind)}")
        if (reconnectRequired) {
            event("reconnecting", "Новое соединение → ${label(kind)}")
            current.reconnect(binder(network))
            reconnectRequired = false
            lastReplyAt = now()
            nextExitCheckAt = 0L
        } else current.migrateTo(key(network), binder(network))
        preparedNetwork = null
        preparedAt = 0
        lastSwitchAt = now()
        lastEchoAt = now()
        nextProbeAt = now() + 500
        activeKind = kind
        activeNetwork = network
        failedNetwork = null
        selected(network, kind)
        manualCellUntil = 0L
        refreshReserve()
        event("active_network", label(kind))
    }

    private fun recover(reason: String) {
        if (isBond) return
        if (!automatic || !alive || now() - lastAttemptAt < 750) return
        lastAttemptAt = now()
        failedNetwork = activeNetwork
        refreshReserve()
        val previous = activeNetwork
        val candidate =
            listOf(WIFI, CELLULAR)
                .mapNotNull { kind ->
                    networks[kind]
                        ?.takeIf { it != activeNetwork && (kind != CELLULAR || cellAllowed()) && (retryAt[it] ?: 0L) <= now() }
                        ?.let { kind to it }
                }
                .firstOrNull()
                ?: if ((reconnectRequired || config.optString("transport") == "https" || isAWG) && failedNetwork != null)
                    networks[activeKind]
                        ?.takeIf { (activeKind != CELLULAR || cellAllowed()) && (retryAt[it] ?: 0L) <= now() }
                        ?.let { activeKind to it }
                else null
        if (candidate == null) {
            if (failedNetwork != previous)
                event("waiting_network", "$reason. Ожидаем доступную сеть для восстановления VPN.")
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
        if (pendingCellStart != null && now() >= manualCellUntil) {
            pendingCellStart = null
            event("operation_failed", "Мобильная сеть недоступна")
        }
        refreshReserve()
        if (!alive) return
        if (!cellAllowed()) {
            pendingCellStart = null
            if (!budgetReported) { budgetReported = true; event("budget_blocked", "Общий лимит LTE исчерпан. Доступен только Wi-Fi. ${budget?.snapshot()}") }
            if (!isBond && activeKind == CELLULAR) { recover("Лимит LTE исчерпан"); return }
        }
        val time = now()
        if (config.optBoolean("probe_exit_ip") && time >= nextExitCheckAt && time - lastEchoAt < 2000) {
            client?.checkExitIP()
            nextExitCheckAt = time + 60000
        }
        if (isBond) {
            if (client?.isConnected() != true && time-lastAttemptAt>5000) {
                lastAttemptAt=time
                val network=networks[WIFI] ?: networks[CELLULAR]?.takeIf { cellAllowed() && client?.bondCellAllowed()!=false }
                if (network != null) try {
                    event("reconnecting", "Общая сессия завершена; старые потоки закрыты")
                    client?.restartBond(if(network==networks[WIFI]) "wifi" else "cell",binder(network)); bondNetworks.clear(); bondRetry.clear()
                } catch(e:Exception) { event("operation_failed", e.message ?: "Reconnect failed") }
            }
            refreshBondPaths()
            return
        }
        if (!automatic) return
        if (!isAWG && (client?.isConnected() != true || time - lastReplyAt > maxOf(15000, intervalMS * 4))) {
            reconnectRequired = true
            failedNetwork = activeNetwork
            recover("Нет рабочего транспортного соединения")
            return
        }
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
        if ((!isAWG || awgPingSeen) && policy.isStalled(time - lastEchoAt, smoothedRTT, intervalMS)) {
            recover("Нет ответов сервера: ${time - lastEchoAt} мс")
        } else if (failedNetwork != null) {
            recover("Текущий путь недоступен")
        }
        if (isAWG) {
            // A second socket to the same AWG peer would move its return endpoint.
            // Prefer a validated Wi-Fi only after dwell; no standby AWG traffic.
            val wifi = networks[WIFI]
            if (wifi != null && wifi in validated && wifi != activeNetwork && candidateAllowed(WIFI, wifi)) {
                readySince.putIfAbsent(wifi, time)
                if ((retryAt[wifi] ?: 0L) <= time && policy.canPreferWifi(time - (readySince[wifi] ?: time), time - lastSwitchAt))
                    try { migrate(WIFI, wifi, "Wi-Fi устойчиво доступен") }
                    catch (e: Exception) { penalize(wifi); event("auto_migration_failed", e.message ?: "AWG rebind failed") }
            }
            return
        }
        if (!alive || time < nextProbeAt) return
        val reserve =
            networks.entries.firstOrNull {
                it.value != activeNetwork && candidateAllowed(it.key, it.value) && (retryAt[it.value] ?: 0L) <= time
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
                    if (kind == CELLULAR) {
                        val pending = pendingCellStart
                        pendingCellStart = null
                        pending?.invoke()
                    }
                    availability(network, kind, true)
                    event("network_available", "${label(kind)} $network")
                }

                override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) =
                    submit {
                        if (caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED)) unmetered.add(network) else unmetered.remove(network)
                        refreshReserve()
                        val ready = caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)
                        validation(network, kind, ready)
                        val newlyReady =
                            if (ready) validated.add(network)
                            else {
                                validated.remove(network)
                                readySince.remove(network)
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
                    unmetered.remove(network)
                    readySince.remove(network)
                    retryAt.remove(network)
                    failures.remove(network)
                    if (preparedNetwork == network) preparedNetwork = null
                    client?.invalidatePath(key(network))
                    event("network_lost", "${label(kind)} $network")
                    if (isBond) {
                        client?.dropBondPath(if(kind==WIFI) "wifi" else "cell")
                        bondNetworks.remove(kind); bondRetry.remove(kind)
                    }
                    if (activeNetwork == network && !isBond) {
                        retryAt.clear()
                        failedNetwork = network
                        recover("Текущая сеть потеряна")
                    }
                }

                override fun onUnavailable() = submit { event("network_unavailable", label(kind)) }
            }
        try {
            cm.registerNetworkCallback(
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
        bondNetworks.clear(); bondRetry.clear()
        awgPingSeen = false
        alive = false
        reconnectRequired = false
        activeNetwork = null
        failedNetwork = null
        preparedNetwork = null
        readySince.clear()
        retryAt.clear()
        failures.clear()
        lastAttemptAt = 0
        nextProbeAt = 0
        client?.let { detached?.invoke(it) }
        client?.stop()
        client = null
        manualCellUntil = 0L
        pendingCellStart = null
        cellRequest?.let { try { cm.unregisterNetworkCallback(it) } catch (_: IllegalArgumentException) {} }
        cellRequest = null
    }

    override fun close() {
        synchronized(worker) {
            if (closed) return
            closed = true
            service?.let {
                it.unregisterReceiver(rttReceiver)
                VpnRttSettings.preferences(it).unregisterOnSharedPreferenceChangeListener(rttListener)
                VpnReserveSettings.preferences(it).unregisterOnSharedPreferenceChangeListener(reserveListener)
            }
            callbacks.forEach { callback ->
                try { cm.unregisterNetworkCallback(callback) } catch (_: IllegalArgumentException) { }
            }
            worker.execute { stopInternal() }
            worker.shutdown()
        }
    }

    private fun binder(network: Network): SocketBinder {
        val raw =
        object : SocketBinder {
            override fun bind(fd: Long) {
                check(service?.protect(fd.toInt()) ?: (attached != null)) { "Cannot protect tunnel socket" }
                ParcelFileDescriptor.fromFd(fd.toInt()).use {
                    network.bindSocket(it.fileDescriptor)
                }
            }
        }

        if (budget == null) return raw
        val caps = cm.getNetworkCapabilities(network)
        val kind = when {
            caps?.hasTransport(CELLULAR) == true -> "cell"
            caps?.hasTransport(WIFI) == true -> "wifi"
            else -> error("Физическая сеть недоступна")
        }
        return budget.bind(raw, kind)
    }

    companion object {
        const val WIFI = NetworkCapabilities.TRANSPORT_WIFI
        const val CELLULAR = NetworkCapabilities.TRANSPORT_CELLULAR

        fun label(kind: Int) = if (kind == WIFI) "Wi-Fi" else "LTE/Cellular"
    }
}
