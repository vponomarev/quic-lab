package ru.vpnc.quiclab

internal data class VpnNotificationContent(val title: String, val text: String, val details: String)

internal fun vpnNetworkLabel(value: String): String = when {
    value.startsWith("Wi-Fi") -> "WiFi"
    value.startsWith("LTE") -> "LTE/сотовая"
    value == "—" -> "Выбор сети"
    else -> value
}

internal fun vpnBytes(bytes: Double): String = when {
    bytes >= 1024 * 1024 -> "%.1f MiB".format(bytes / (1024 * 1024))
    bytes >= 1024 -> "%.1f KiB".format(bytes / 1024)
    else -> "%.0f B".format(bytes)
}

internal fun vpnTrafficRate(tx: Double, rx: Double) = "↑ ${vpnBytes(tx)}/с · ↓ ${vpnBytes(rx)}/с"
