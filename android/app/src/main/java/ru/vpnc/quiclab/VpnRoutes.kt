package ru.vpnc.quiclab
internal object VpnRoutes {
    fun parse(raw: String): List<Pair<String, Int>> {
        val result =
            raw.split(Regex("[\\s,;]+"))
                .filter { it.isNotBlank() }
                .map { entry ->
                    val parts = entry.split("/")
                    require(parts.size == 2) { "Нужен IPv4 CIDR: $entry" }
                    val bits = parts[1].toIntOrNull() ?: error("Неверная маска: $entry")
                    require(bits in 0..32) { "Неверная маска: $entry" }
                    val octets = parts[0].split(".").map { it.toIntOrNull() ?: -1 }
                    require(octets.size == 4 && octets.all { it in 0..255 }) {
                        "Неверный IPv4: $entry"
                    }
                    val ip = octets.fold(0L) { n, v -> (n shl 8) or v.toLong() }
                    val mask = if (bits == 0) 0L else (0xffffffffL shl (32 - bits)) and 0xffffffffL
                    val network = ip and mask
                    (listOf(24, 16, 8, 0).joinToString(".") {
                        ((network ushr it) and 255).toString()
                    }) to bits
                }
                .distinct()
        require(result.isNotEmpty()) { "Добавьте хотя бы одну подсеть" }
        require(result.size <= 128) { "Не более 128 маршрутов" }
        return result
    }
}
