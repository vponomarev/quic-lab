package ru.vpnc.quiclab

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.VpnService
import android.os.ParcelFileDescriptor
import mobile.Mobile
import mobile.SocketBinder
import mobile.TrafficBudget
import mobile.ServiceTransferTrust
import org.json.JSONObject
import java.io.File
import java.net.Inet4Address
import java.net.URI
import java.util.concurrent.ConcurrentHashMap

/** Narrow bridge for D4: caller supplies a verified profile ID and physical
 * Network. Explicit consent applies to this ID only; retry always starts a fresh
 * temporary file. Config/APK contents must still be validated by D4 before use. */
internal class ServiceTransfer(
    private val context: Context,
    private val budget: TrafficBudget,
    private val vpn: VpnService? = null,
) {
    private val active = ConcurrentHashMap<String, mobile.ServiceTransfer>()

    fun fetch(operationID: String, profileID: String, kind: String, output: File,
              network: Network, allowOverBudget: Boolean = false, authenticatedAPKURL: String? = null) {
        require(kind == "config" || kind == "apk")
        val transfer = Mobile.newServiceTransfer(budget, null)
        check(active.putIfAbsent(operationID, transfer) == null) { "Операция уже выполняется" }
        try {
            val cm = context.getSystemService(ConnectivityManager::class.java)
            val caps = requireNotNull(cm.getNetworkCapabilities(network))
            require(!caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) { "Нужна физическая сеть" }
            val physical = when {
                caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cell"
                caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
                else -> error("Нужна Wi-Fi или LTE сеть")
            }
            // This metadata is retained only in the encrypted identity bundle after
            // verified import. Never infer an update host from a data-plane endpoint.
            val identity = VpnIdentity.load(context, profileID)
            validateMetadata(identity)
            require(authenticatedAPKURL == null || kind == "apk")
            val url = authenticatedAPKURL ?: identity.getString(if (kind == "config") "config_url" else "apk_url")
            if (authenticatedAPKURL != null) validateMetadata(JSONObject().put("apk_url",authenticatedAPKURL))
            require(url.isNotEmpty()) { "В профиле нет проверенного адреса обновления" }
            val token = identity.optString("update_token")
            if (kind == "config") require(token.isNotEmpty()) { "Нет ключа обновления устройства" }

            transfer.setTrust(object : ServiceTransferTrust {
                override fun approvedURL(requestKind: String, raw: String) = requestKind == kind && raw == url
                override fun credentials(reference: String): String {
                    require(reference == profileID)
                    // Updates use ordinary HTTPS PKI independently of VPN cover SNI.
                    return JSONObject().put("token", if (kind == "config") token else "").toString()
                }
                override fun resolveAddress(host: String, port: Long): String {
                    val address = network.getAllByName(host).firstOrNull { it is Inet4Address }
                        ?: error("Нет IPv4 адреса в выбранной сети")
                    return "${address.hostAddress}:$port"
                }
            })
            val binder = object : SocketBinder {
                override fun bind(fd: Long) {
                    if (LabVpnService.active) check(vpn?.protect(fd.toInt()) == true) { "Не удалось исключить служебный socket из VPN" }
                    ParcelFileDescriptor.fromFd(fd.toInt()).use { network.bindSocket(it.fileDescriptor) }
                }
            }
            if (allowOverBudget) budget.grantTransfer(operationID)
            val request = JSONObject().put("id", operationID).put("kind", kind)
                .put("https_url", url).put("output_file", output.absolutePath)
                .put("device_auth_ref", profileID)
            transfer.fetch(request.toString(), budget.bind(binder, physical))
        } finally {
            transfer.cancel(operationID)
            active.remove(operationID, transfer)
        }
    }

    fun cancel(operationID: String) {
        budget.revokeTransfer(operationID)
        active[operationID]?.cancel(operationID)
    }

    companion object {
        fun validateMetadata(p: JSONObject) {
            for (key in listOf("config_url", "apk_url")) {
                val raw = p.optString(key)
                if (raw.isEmpty()) continue
                val u = URI(raw)
                require(u.scheme == "https" && u.host != null && u.userInfo == null &&
                    u.rawQuery == null && u.fragment == null && !u.isOpaque &&
                    (u.port == -1 || u.port in 1..65535)) { "Неверный проверенный адрес $key" }
            }
            val token = p.optString("update_token")
            require(token.length <= 4096 && !token.contains('\r') && !token.contains('\n')) { "Неверный ключ обновления" }
            if (p.optString("config_url").isNotEmpty()) require(token.isNotEmpty()) { "Адрес конфигурации требует ключ обновления" }
        }
    }
}
