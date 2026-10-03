package ru.vpnc.quiclab

import org.json.JSONObject

/** TCP sockets and end-to-end proof are independent; no implied VLESS authentication. */
internal class VlessConnectionStatus {
    private var tcp = "подключение…"
    private var tunnel = "проверяется…"
    private var verifiedAt: Long? = null
    fun accept(event: JSONObject, now: Long) {
        when (event.optString("event")) {
            "connected", "reconnecting", "waiting_network" -> {
                tcp = "подключение…"; tunnel = "проверяется…"; verifiedAt = null
            }
            "disconnected", "session_closed", "session_lost" -> {
                tcp = "закрыт"; tunnel = "проверка не прошла"; verifiedAt = null
            }
            "vless_tcp" -> tcp = when (event.optString("tcp_state")) {
                "established" -> "установлен"
                "connecting" -> "подключение…"
                "error" -> "ошибка подключения"
                else -> "закрыт"
            }
            "health" -> { tunnel = "подтверждена"; verifiedAt = now }
            "echo" -> if (event.optDouble("rtt_ms", Double.NaN).let { it.isFinite() && it >= 0 }) {
                tunnel = "подтверждена"; verifiedAt = now
            }
            "probe_unavailable" -> { tunnel = "проверка не прошла"; verifiedAt = null }
        }
    }
    fun label(now: Long): String {
        val proof = if (verifiedAt?.let { now - it > 30_000 } == true) "нет свежего подтверждения" else tunnel
        return "TCP до сервера: $tcp\nПередача через VPN: $proof"
    }
}
