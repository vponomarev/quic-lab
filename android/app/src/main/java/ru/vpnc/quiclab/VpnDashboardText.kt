package ru.vpnc.quiclab

import java.util.Locale
import org.json.JSONObject

/** Stateless presentation: policy changes do not reset counters or manufacture samples. */
internal object VpnDashboardText {
    private fun number(value: Double) = String.format(Locale.US, "%.1f", value)
    fun bytes(value: Long): String = bytes(value.toDouble())
    fun bytes(value: Double): String = when {
        value >= 1024 * 1024 * 1024 -> "${number(value / (1024 * 1024 * 1024))} GiB"
        value >= 1024 * 1024 -> "${number(value / (1024 * 1024))} MiB"
        value >= 1024 -> "${number(value / 1024)} KiB"
        else -> "${value.toLong()} B"
    }
    fun network(raw: String) = when {
        raw.equals("wifi", true) || raw.contains("Wi-Fi", true) -> "Wi-Fi"
        raw.equals("cell", true) || raw.contains("LTE", true) || raw.contains("Mobile", true) -> "LTE"
        raw.isBlank() -> "Недоступно"
        else -> raw
    }
    private fun fresh(path: PathSnapshot?, now: Long, interval: Long) = interval > 0 && path != null &&
        path.rttMs?.let { it.isFinite() && it >= 0 } == true && path.measuredAtMs > 0 &&
        now >= path.measuredAtMs && now - path.measuredAtMs <= maxOf(3 * interval, 5000) &&
        path.state != "Нет свежих ответов"
    fun latency(path: PathSnapshot?, now: Long, interval: Long): String = when {
        interval == 0L -> "RTT выключен"
        !fresh(path, now, interval) -> "Недоступно"
        else -> "${number(requireNotNull(path?.rttMs))} мс"
    }
    fun card(exit: ExitDashboard, name: String, now: Long, interval: Long, radio: String, profileLabels: Map<String, String> = emptyMap()): String {
        fun profile(id: String) = profileLabels[id] ?: "Профиль"
        val path = exit.paths.firstOrNull { it.pathId == exit.activePathId }
        val jitter = path?.jitterMs?.takeIf { fresh(path, now, interval) && it.isFinite() && it >= 0 }
        val active = path?.let { "${profile(it.profileId)} · ${network(it.network)}" } ?: "Недоступно"
        val alternatives = exit.paths.filter { it.pathId != exit.activePathId }.joinToString("\n") {
            "${profile(it.profileId)} · ${network(it.network)}: ${it.state} · RTT ${latency(it, now, interval)}"
        }
        return buildString {
            append("$name · ${exit.state}\n")
            append("RX ↓ ${bytes(exit.rxBps)}/с    TX ↑ ${bytes(exit.txBps)}/с\n")
            append("За запуск: ↓ ${bytes(exit.rxTotalBytes)}    ↑ ${bytes(exit.txTotalBytes)}\n")
            append("Активный путь: $active\nRTT: ${latency(path, now, interval)}\n")
            append("Jitter max · 15 с: ${if (interval == 0L) "выключен" else jitter?.let { "${number(it)} мс" } ?: "Недоступно"}\n")
            append("Exit IPv4: ${exit.exitIpv4 ?: "Недоступен"}")
            if (radio.isNotBlank()) append("\nРадио: $radio")
            if (alternatives.isNotBlank()) append("\nАльтернативы:\n$alternatives")
        }
    }
    fun budget(snapshot: JSONObject?): String {
        if (snapshot == null) return "Общий LTE бюджет: VPN выключен"
        val used = bytes(snapshot.optLong("used")); val limit = snapshot.optLong("limit")
        return "LTE за период: $used · ${if (limit == 0L) "без ограничения" else "лимит ${bytes(limit)}"}" +
            if (snapshot.optBoolean("blocked")) "\nЛимит исчерпан · остаётся Wi-Fi" else ""
    }
}
