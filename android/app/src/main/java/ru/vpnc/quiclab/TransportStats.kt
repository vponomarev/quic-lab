package ru.vpnc.quiclab

import org.json.JSONObject

/** Main-thread model. Echo gaps span WebSocket reconnects; RTT never includes UI time. */
internal class TransportStats {
    private data class RttSample(val time: Long, val value: Double)
    private val recentRtt = ArrayDeque<RttSample>()
    private var stoppedAt: Long? = null
    private fun trimRtt(now: Long) {
        while (recentRtt.isNotEmpty() && recentRtt.first().time < now - 15000) recentRtt.removeFirst()
    }
    /** Both samples must be in the window; reconnects preserve RTT history. */
    fun maxJitter(now: Long): Double? {
        trimRtt(stoppedAt ?: now)
        if (recentRtt.size < 2) return null
        var previous: Double? = null
        var maximum = 0.0
        recentRtt.forEach { sample ->
            previous?.let { maximum = maxOf(maximum, kotlin.math.abs(sample.value - it)) }
            previous = sample.value
        }
        return maximum
    }
    var state = "Готов к запуску"
    var network = "—"
    var rtt = 0.0
    var maxGap = 0.0
    var lastEcho = 0L
    var replies = 0L
    var migrations = 0
    var active = false
    val sessions = linkedSetOf<String>()
    fun accept(e: JSONObject, now: Long) {
        when (e.optString("event")) {
            "connecting" -> { active = true; state = "Подключение…" }
            "connected" -> { active = true; state = "Подключён" }
            "echo" -> {
                rtt = e.optDouble("rtt_ms")
                if (rtt.isFinite() && rtt >= 0) {
                    trimRtt(now)
                    recentRtt.addLast(RttSample(now, rtt))
                }
                if (lastEcho != 0L) maxGap = maxOf(maxGap, (now - lastEcho).toDouble(), (e.optDouble("gap_ms", 0.0).takeIf { it.isFinite() && it >= 0 } ?: 0.0))
                lastEcho = now
                replies++
                e.optString("connection_id").takeIf { it.isNotBlank() }?.let { sessions.add(it) }
                state = "Ответы приходят"
            }
            "active_network" -> network = e.optString("detail")
            "migration_started" -> state = "Меняем путь…"
            "path_switched" -> migrations++
            "reconnecting" -> state = "Новое соединение…"
            "disconnected", "session_closed" -> { active = false; state = "Соединение закрыто" }
            "operation_failed", "auto_migration_failed" -> state = "Не удалось подключиться"
            "waiting_network" -> state = "Ждём доступную сеть"
            "stopped" -> { stoppedAt = now; active = false; state = "Опыт завершён" }
        }
    }
    fun silence(now: Long) = if (active && lastEcho != 0L) now - lastEcho else 0L
}
