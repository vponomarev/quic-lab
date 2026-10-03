package ru.vpnc.quiclab

import android.os.SystemClock
import org.json.JSONObject

/** Accepted controller events only. No sockets or transport policy are owned by the dashboard. */
internal class VpnDashboardEventStore {
    private data class Rtt(val at: Long, val value: Double)
    private class Exit(val id: String) {
        var generation = 0L
        var state = "Ожидаем подключения"
        var stopped = false
        var connection = ""
        val retiredConnections = mutableSetOf<String>()
        var activePath: String? = null
        val paths = linkedMapOf<String, PathSnapshot>()
        val pathGenerations = mutableMapOf<String, Long>()
        val samples = mutableMapOf<String, ArrayDeque<Rtt>>()
        var ip: String? = null
        // Router profile_traffic is whole-run cumulative, not Gateway session traffic.
        var rawRx = 0L
        var rawTx = 0L
        var generationRx = 0L
        var generationTx = 0L
        var rxRate = 0.0
        var txRate = 0.0
        var trafficAt = 0L
        var capturedAt = 0L
        fun clearPathMetrics() { activePath = null; paths.clear(); samples.clear(); pathGenerations.clear(); ip = null }
    }
    @Volatile var runId: String? = null
        private set
    private var names = emptyMap<String, String>()
    private val exits = linkedMapOf<String, Exit>()
    private var model = VpnDashboardModel(configuredIntervalMs = 5000)
    private var budgetProvider: (() -> String?)? = null

    @Synchronized fun beginRun(newRunId: String, exitNames: Map<String, String>, provider: (() -> String?)? = null) {
        require(newRunId.isNotBlank())
        runId = newRunId; names = exitNames.toMap(); exits.clear()
        model = VpnDashboardModel(configuredIntervalMs = 5000).also { it.beginRun(newRunId) }
        exitNames.keys.forEach { exits[it] = Exit(it) }
        budgetProvider = provider
    }
    @Synchronized fun setBudgetProvider(provider: (() -> String?)?) { budgetProvider = provider }
    @Synchronized fun names(): Map<String, String> = names.toMap()
    @Synchronized fun budget(): JSONObject? = runCatching { budgetProvider?.invoke()?.let { JSONObject(it) } }.getOrNull()
    @Synchronized fun clearRun() { exits.clear(); names = emptyMap(); model.reset(); runId = null; budgetProvider = null }

    @Synchronized fun record(exitId: String, generation: Long, event: JSONObject) =
        runId?.let { record(it, exitId, generation, event) } ?: false

    @Synchronized fun record(expectedRunId: String, exitId: String, generation: Long, event: JSONObject,
                             nowMs: Long = SystemClock.elapsedRealtime()): Boolean {
        if (expectedRunId != runId) return false
        val exit = exits[exitId] ?: return false
        if (generation < exit.generation || nowMs < exit.capturedAt) return false
        val kind = event.optString("event")
        val connection = event.optString("connection_id")
        if (connection.isNotBlank() && connection in exit.retiredConnections) return false
        if (generation > exit.generation) {
            exit.generation = generation; exit.generationRx = 0; exit.generationTx = 0
            exit.clearPathMetrics(); exit.connection = ""; exit.retiredConnections.clear()
            exit.stopped = false; exit.state = "Подключение…"
        }
        if (exit.stopped && kind != "profile_traffic") return false
        if (kind == "connected" && connection.isNotBlank() && connection != exit.connection) {
            if (exit.connection.isNotBlank()) { exit.retiredConnections.add(exit.connection); exit.clearPathMetrics() }
            exit.connection = connection
        }
        val pathId = event.optString("path_id").ifBlank {
            exit.activePath ?: connection.ifBlank { "active" }
        }
        val pathGeneration = event.optLong("path_generation", exit.pathGenerations[pathId] ?: 0L)
        if (event.has("path_id") && pathGeneration < (exit.pathGenerations[pathId] ?: 0L)) return false
        if (event.has("path_id") && pathGeneration > (exit.pathGenerations[pathId] ?: 0L)) {
            exit.paths.remove(pathId); exit.samples.remove(pathId)
        }
        if (event.has("path_id")) exit.pathGenerations[pathId] = pathGeneration
        fun path(): PathSnapshot = exit.paths[pathId] ?: PathSnapshot(pathId, event.optString("profile_id"),
            event.optString("network"), "Ожидаем измерений", null, null, 0)
        when (kind) {
            "profile_traffic" -> {
                val rx = event.optLong("rx_bytes", -1); val tx = event.optLong("tx_bytes", -1)
                if (rx < exit.rawRx || tx < exit.rawTx) return false
                val seconds = ((nowMs - exit.trafficAt).coerceAtLeast(1) / 1000.0)
                val drx = rx - exit.rawRx; val dtx = tx - exit.rawTx
                exit.generationRx += drx; exit.generationTx += dtx
                exit.rxRate = if (exit.trafficAt == 0L) 0.0 else drx / seconds; exit.txRate = if (exit.trafficAt == 0L) 0.0 else dtx / seconds
                exit.rawRx = rx; exit.rawTx = tx; exit.trafficAt = nowMs
            }
            "traffic" -> return false // The router is the authoritative application-byte counter.
            "connected", "path_switched" -> {
                exit.activePath = pathId; exit.paths[pathId] = path().copy(state = "Активен")
                exit.state = "Подключён"
            }
            "active_network" -> {
                val current = path()
                exit.activePath = pathId
                val network = event.optString("network").ifBlank { event.optString("detail") }
                val changed = VpnDashboardText.network(current.network) != VpnDashboardText.network(network)
                if (changed) exit.samples.remove(pathId)
                exit.paths[pathId] = if (changed) current.copy(network = network, rttMs = null, jitterMs = null, measuredAtMs = 0)
                    else current.copy(network = network)
            }
            "echo" -> {
                val value = event.optDouble("rtt_ms", Double.NaN)
                if (!value.isFinite() || value < 0) return false
                val history = exit.samples.getOrPut(pathId) { ArrayDeque() }
                history.addLast(Rtt(nowMs, value))
                while (history.isNotEmpty() && history.first().at < nowMs - 15_000) history.removeFirst()
                while (history.size > 64) history.removeFirst()
                exit.paths[pathId] = path().copy(rttMs = value, measuredAtMs = nowMs, state = "Готов")
            }
            "bond_stats" -> {
                val array = event.optJSONArray("paths")
                val present = mutableSetOf<String>()
                for (i in 0 until (array?.length() ?: 0)) {
                    val p = array!!.optJSONObject(i) ?: continue
                    val id = p.optString("name")
                    if (id.isBlank()) continue
                    present.add(id)
                    val pg = p.optLong("generation")
                    if (pg < (exit.pathGenerations[id] ?: 0L)) continue
                    if (pg > (exit.pathGenerations[id] ?: 0L)) {
                        exit.paths.remove(id); exit.samples.remove(id)
                    }
                    exit.pathGenerations[id] = pg
                    val previous = exit.paths[id]
                    exit.paths[id] = PathSnapshot(id, p.optString("profile_id"), p.optString("network"),
                        if (p.optBoolean("ready")) "Готов" else "Нет свежих ответов",
                        previous?.rttMs, previous?.jitterMs, previous?.measuredAtMs ?: 0)
                }
                exit.paths.keys.filter { it !in present }.forEach { exit.paths.remove(it); exit.samples.remove(it) }
            }
            "rtt_policy" -> if (!event.optBoolean("enabled")) {
                exit.samples.clear()
                exit.paths.replaceAll { _, p -> p.copy(rttMs = null, jitterMs = null, measuredAtMs = 0) }
            }
            "exit_ip" -> exit.ip = event.optString("ip").takeIf { it.isNotBlank() }
            "exit_ip_checking", "exit_ip_failed" -> exit.ip = null
            "operation_failed" -> {
                // A pool candidate can fail while the preferred path continues serving traffic.
                if (!event.has("path_id") || exit.activePath == null || pathId == exit.activePath) {
                    exit.state = "Восстанавливаем подключение"; exit.ip = null
                }
            }
            "reconnecting", "disconnected", "session_closed", "session_lost", "waiting_network" -> {
                exit.state = "Восстанавливаем подключение"; exit.ip = null
                if (kind in listOf("session_closed", "session_lost", "disconnected")) exit.clearPathMetrics()
            }
        }
        exit.capturedAt = nowMs
        return model.accept(snapshot(exit, nowMs))
    }
    @Synchronized fun stopExit(exitId: String, generation: Long) {
        val exit = exits[exitId] ?: return
        if (generation < exit.generation) return
        if (generation > exit.generation) { exit.generationRx = 0; exit.generationTx = 0 }
        exit.generation = generation; exit.stopped = true; exit.state = "Выход остановлен · трафик блокируется"
        exit.clearPathMetrics(); exit.rxRate = 0.0; exit.txRate = 0.0
        model.accept(snapshot(exit, SystemClock.elapsedRealtime()))
    }
    private fun snapshot(exit: Exit, nowMs: Long): ExitSnapshot {
        val paths = exit.paths.values.map { path ->
            val history = exit.samples[path.pathId]
            while (history != null && history.isNotEmpty() && history.first().at < nowMs - 15_000) history.removeFirst()
            val jitter = history?.zipWithNext { a, b -> kotlin.math.abs(b.value - a.value) }?.maxOrNull()
            path.copy(jitterMs = jitter)
        }
        val freshTraffic = !exit.stopped && exit.trafficAt > 0 && nowMs - exit.trafficAt <= 3500
        return ExitSnapshot(exit.id, exit.generation, exit.state, exit.activePath, paths,
            exit.generationRx, exit.generationTx, if (freshTraffic) exit.rxRate else 0.0,
            if (freshTraffic) exit.txRate else 0.0, exit.ip, runId.orEmpty(), nowMs)
    }
    @Synchronized fun snapshots(nowMs: Long = SystemClock.elapsedRealtime()): List<ExitSnapshot> = exits.values.map { snapshot(it, nowMs) }
    @Synchronized fun dashboards(nowMs: Long = SystemClock.elapsedRealtime()): List<ExitDashboard> = exits.values.mapNotNull {
        model.accept(snapshot(it, nowMs)); model.exit(it.id)
    }
}

internal object VpnDashboardEvents {
    private val store = VpnDashboardEventStore()
    val runId: String? get() = store.runId
    fun beginRun(runId: String, exits: Map<String, String>, budgetProvider: (() -> String?)? = null) = store.beginRun(runId, exits, budgetProvider)
    fun setBudgetProvider(provider: (() -> String?)?) = store.setBudgetProvider(provider)
    fun record(exitId: String, generation: Long, event: JSONObject) = store.record(exitId, generation, event)
    fun record(runId: String, exitId: String, generation: Long, event: JSONObject) = store.record(runId, exitId, generation, event)
    fun stopExit(exitId: String, generation: Long) = store.stopExit(exitId, generation)
    fun clearRun() = store.clearRun()
    fun names() = store.names()
    fun budget() = store.budget()
    fun snapshots() = store.snapshots()
    fun dashboards() = store.dashboards()
}
