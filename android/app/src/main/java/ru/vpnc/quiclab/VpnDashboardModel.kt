package ru.vpnc.quiclab

/** Immutable network path sample delivered by one exit. */
data class PathSnapshot(
    val pathId: String,
    val profileId: String,
    val network: String,
    val state: String,
    val rttMs: Double?,
    val jitterMs: Double?,
    val measuredAtMs: Long,
)

/** Immutable per-exit sample. Counters are scoped to [generation] by the producer. */
data class ExitSnapshot(
    val exitId: String,
    val generation: Long,
    val state: String,
    val activePathId: String?,
    val paths: List<PathSnapshot>,
    val rxBytes: Long,
    val txBytes: Long,
    val rxBps: Double,
    val txBps: Double,
    val exitIpv4: String?,
    val runId: String = "",
    val capturedAtMs: Long = 0L,
    val radio: RadioSnapshot? = null,
)

data class RadioSnapshot(
    val network: String,
    val signalDbm: Int?,
    val measuredAtMs: Long,
)

/** Values suitable for rendering one exit card. */
data class ExitDashboard(
    val exitId: String,
    val runId: String,
    val generation: Long,
    val state: String,
    val activePathId: String?,
    val paths: List<PathSnapshot>,
    val rxBytes: Long,
    val txBytes: Long,
    val rxBps: Double,
    val txBps: Double,
    val rxTotalBytes: Long,
    val txTotalBytes: Long,
    val exitIpv4: String?,
    val rttMs: Double?,
    val jitterMs: Double?,
    val radio: RadioSnapshot?,
    val rttLabel: String,
    val radioLabel: String,
    private val configuredIntervalMs: Long,
    private val capturedAtMs: Long,
    private val rttEnabled: Boolean,
) {
    fun rttLabelAt(nowMs: Long): String = if (!rttEnabled) {
        "RTT выключен"
    } else if (rttMs == null || nowMs - capturedAtMs > maxOf(3L * configuredIntervalMs, 5_000L)) {
        "Недоступно"
    } else {
        "${rttMs} мс"
    }

    fun rttMsAt(nowMs: Long): Double? =
        if (rttEnabled && rttMs != null && nowMs - capturedAtMs <= maxOf(3L * configuredIntervalMs, 5_000L)) rttMs else null
}

/**
 * Pure reducer for exit cards. It does not make transport decisions or depend on Android.
 * A run ID starts a fresh set of totals; transport generations only delimit counter deltas.
 */
class VpnDashboardModel(
    private val configuredIntervalMs: Long = 1_000L,
    private val rttEnabled: Boolean = true,
    private val radioPermissionGranted: Boolean = true,
) {
    private data class State(
        var snapshot: ExitSnapshot,
        var lastGeneration: Long,
        var lastRx: Long,
        var lastTx: Long,
        var lastCapturedAtMs: Long,
        var rxTotal: Long,
        var txTotal: Long,
    )

    private val exits = linkedMapOf<String, State>()
    private var runId: String? = null

    fun beginRun(newRunId: String) {
        exits.clear()
        runId = newRunId
    }

    /** Accepts a fresh sample, returning false for an older run or generation. */
    fun accept(snapshot: ExitSnapshot): Boolean {
        if (snapshot.rxBytes < 0L || snapshot.txBytes < 0L ||
            !snapshot.rxBps.isFinite() || snapshot.rxBps < 0.0 ||
            !snapshot.txBps.isFinite() || snapshot.txBps < 0.0
        ) return false
        val currentRun = runId
        if (currentRun != null && currentRun != snapshot.runId) return false
        if (currentRun == null) runId = snapshot.runId
        val previous = exits[snapshot.exitId]
        if (previous != null && snapshot.generation < previous.lastGeneration) return false
        if (previous != null && snapshot.capturedAtMs < previous.lastCapturedAtMs) return false
        if (previous != null && snapshot.generation == previous.lastGeneration &&
            (snapshot.rxBytes < previous.lastRx || snapshot.txBytes < previous.lastTx)) return false

        val state = if (previous == null) {
            State(snapshot.copy(paths = snapshot.paths.toList()), snapshot.generation, snapshot.rxBytes, snapshot.txBytes, snapshot.capturedAtMs,
                snapshot.rxBytes.coerceAtLeast(0L), snapshot.txBytes.coerceAtLeast(0L))
        } else {
            val generationChanged = snapshot.generation > previous.lastGeneration
            val rxDelta = if (generationChanged) snapshot.rxBytes else (snapshot.rxBytes - previous.lastRx).coerceAtLeast(0L)
            val txDelta = if (generationChanged) snapshot.txBytes else (snapshot.txBytes - previous.lastTx).coerceAtLeast(0L)
            previous.apply {
                this.snapshot = snapshot.copy(paths = snapshot.paths.toList())
                this.lastGeneration = snapshot.generation
                this.lastRx = snapshot.rxBytes
                this.lastTx = snapshot.txBytes
                this.lastCapturedAtMs = snapshot.capturedAtMs
                this.rxTotal += rxDelta
                this.txTotal += txDelta
            }
            previous
        }
        exits[snapshot.exitId] = state
        return true
    }

    fun exit(exitId: String): ExitDashboard? = exits[exitId]?.let { it.toDashboard() }

    fun reset() {
        exits.clear()
        runId = null
    }

    private fun State.toDashboard(): ExitDashboard {
        val s = snapshot
        val path = s.paths.firstOrNull { it.pathId == s.activePathId }
        val rtt = path?.rttMs?.takeIf { it.isFinite() && it >= 0.0 }
        val fresh = rtt != null && s.capturedAtMs - path.measuredAtMs <= maxOf(3L * configuredIntervalMs, 5_000L)
        return ExitDashboard(
            exitId = s.exitId, runId = s.runId, generation = lastGeneration, state = s.state,
            activePathId = s.activePathId, paths = s.paths, rxBytes = s.rxBytes, txBytes = s.txBytes,
            rxBps = s.rxBps, txBps = s.txBps, rxTotalBytes = rxTotal, txTotalBytes = txTotal,
            exitIpv4 = s.exitIpv4, rttMs = rtt?.takeIf { rttEnabled && fresh },
            jitterMs = path?.jitterMs?.takeIf { rttEnabled && fresh && it.isFinite() && it >= 0.0 },
            radio = s.radio?.takeIf { radioPermissionGranted }, rttLabel = when {
                !rttEnabled -> "RTT выключен"
                !fresh -> "Недоступно"
                else -> "${rtt} мс"
            }, radioLabel = if (!radioPermissionGranted || s.radio == null) "Недоступно" else s.radio.network,
            configuredIntervalMs = configuredIntervalMs, capturedAtMs = path?.measuredAtMs ?: s.capturedAtMs,
            rttEnabled = rttEnabled,
        )
    }
}
