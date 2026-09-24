package ru.vpnc.quiclab

import android.net.Network
import java.net.Inet4Address
import java.net.URI

internal data class ServerEndpoint(val address: String, val hostname: String)

// Resolve using the same Android Network that will carry the initial connection.
internal fun resolveEndpoint(value: String, network: Network): ServerEndpoint {
    val uri = URI("quic://$value")
    require(uri.userInfo == null && uri.rawQuery == null && uri.rawFragment == null && uri.path.isNullOrEmpty()) {
        "Укажите только имя или IP сервера и порт"
    }
    val host = requireNotNull(uri.host) { "Нужен адрес сервера: имя:порт" }.removeSurrounding("[", "]")
    require(uri.port in 1..65535) { "Порт должен быть от 1 до 65535" }
    val addresses = network.getAllByName(host)
    val ip = (addresses.firstOrNull { it is Inet4Address } ?: addresses.first()).hostAddress!!
    return ServerEndpoint(if (ip.contains(':')) "[$ip]:${uri.port}" else "$ip:${uri.port}", host)
}
