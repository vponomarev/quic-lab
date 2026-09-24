package ru.vpnc.quiclab

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.location.LocationManager
import android.net.*
import android.net.wifi.WifiInfo
import android.net.wifi.WifiManager
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.telephony.*

/** Foreground diagnostics only. Never scans for neighbouring Wi-Fi networks. */
internal class RadioMonitor(private val context: Context,
    private val display: (String, String) -> Unit,
    private val transition: (String, Long) -> Unit) {
    private val handler = Handler(Looper.getMainLooper())
    private val cm = context.getSystemService(ConnectivityManager::class.java)
    private val wifi = context.applicationContext.getSystemService(WifiManager::class.java)
    private var callback: ConnectivityManager.NetworkCallback? = null
    private var wifiText = "Wi-Fi: сведения недоступны"
    private var cellText = "Сотовая сеть: сведения недоступны"
    private var lastWifi: String? = null
    private var lastCell: String? = null
    private var cellPoll = 0L
    private var closed = false
    private var pending = false
    private var latestCells: List<CellInfo> = emptyList()
    private fun permitted() = context.checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION) == PackageManager.PERMISSION_GRANTED
    private fun id(n: Int) = if (n == Int.MAX_VALUE || n < 0) "—" else n.toString()
    private fun id(n: Long) = if (n == Long.MAX_VALUE || n < 0) "—" else n.toString()

    fun start() {
        val cb = object : ConnectivityManager.NetworkCallback(FLAG_INCLUDE_LOCATION_INFO) {
            override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) {
                val info = caps.transportInfo as? WifiInfo ?: return
                handler.post { if (!closed) showWifi(info) }
            }
        }
        try {
            cm.registerNetworkCallback(NetworkRequest.Builder().addTransportType(NetworkCapabilities.TRANSPORT_WIFI).build(), cb)
            callback = cb
        } catch (_: SecurityException) { }
        handler.post(poll)
    }

    private fun changed(wifi: Boolean, key: String, message: String) {
        val old = if (wifi) lastWifi else lastCell
        if (old != null && old != key) transition(message, SystemClock.elapsedRealtime())
        if (wifi) lastWifi = key else lastCell = key
    }

    private fun showWifi(info: WifiInfo?) {
        if (closed) return
        val bssid = info?.bssid?.takeUnless { it == "02:00:00:00:00:00" }
        val ssid = info?.ssid?.takeUnless { it == WifiManager.UNKNOWN_SSID }?.trim('"')
        if (bssid == null) {
            wifiText = "Wi-Fi: ${if (permitted()) "нет подключения или Android скрывает данные" else "SSID/BSSID требуют точную геопозицию"}"
        } else {
            changed(true, "$ssid/$bssid", "Wi-Fi: ${ssid ?: "скрытый SSID"} · BSSID $bssid")
            wifiText = "Wi-Fi: ${ssid ?: "скрытый SSID"}\nBSSID (MAC точки): $bssid\nСигнал ${info.rssi} dBm · ${info.frequency} МГц · линк ${info.linkSpeed} Мбит/с"
        }
        display(wifiText, cellText)
    }

    private fun showCells(cells: List<CellInfo>) {
        if (closed) return
        val registered = cells.filter { it.isRegistered }
        if (registered.isEmpty()) { cellText = "Сота: Android пока не сообщил обслуживающую соту"; display(wifiText, cellText); return }
        val identities = mutableListOf<String>()
        val lines = registered.map { cell ->
            val identity = when (cell) {
                is CellInfoLte -> cell.cellIdentity.let { "LTE · ${it.mccString ?: "?"}/${it.mncString ?: "?"} · CI ${id(it.ci)} · TAC ${id(it.tac)} · PCI ${id(it.pci)} · EARFCN ${id(it.earfcn)}" }
                is CellInfoNr -> (cell.cellIdentity as CellIdentityNr).let { "NR · ${it.mccString ?: "?"}/${it.mncString ?: "?"} · NCI ${id(it.nci)} · TAC ${id(it.tac)} · PCI ${id(it.pci)} · NRARFCN ${id(it.nrarfcn)}" }
                is CellInfoGsm -> cell.cellIdentity.let { "GSM · ${it.mccString ?: "?"}/${it.mncString ?: "?"} · CID ${id(it.cid)} · LAC ${id(it.lac)}" }
                is CellInfoWcdma -> cell.cellIdentity.let { "WCDMA · ${it.mccString ?: "?"}/${it.mncString ?: "?"} · CID ${id(it.cid)} · LAC ${id(it.lac)}" }
                else -> cell.javaClass.simpleName
            }
            identities.add(identity)
            val age = ((SystemClock.elapsedRealtime() - cell.timestampMillis).coerceAtLeast(0) / 1000)
            val dbm = cell.cellSignalStrength.dbm.takeUnless { it == Int.MAX_VALUE }?.toString() ?: "—"
            "$identity\nСигнал $dbm dBm · данные $age с назад"
        }
        changed(false, identities.sorted().joinToString("|"), "Смена обслуживающей соты: ${identities.joinToString("; ")}")
        cellText = "Сотовая сеть (SIM данных):\n" + lines.joinToString("\n")
        display(wifiText, cellText)
    }

    private val poll = object : Runnable {
        @Suppress("DEPRECATION", "MissingPermission")
        override fun run() {
            if (closed) return
            val locationOn = context.getSystemService(LocationManager::class.java).isLocationEnabled
            if (!permitted() || !locationOn) {
                wifiText = "Wi-Fi SSID/BSSID: ${if (!permitted()) "нужно разрешение точной геопозиции" else "включите геолокацию Android"}"
                cellText = "Сота: ${if (!permitted()) "нужно разрешение точной геопозиции" else "включите геолокацию Android"}"
                display(wifiText, cellText)
            } else {
                try { showWifi(wifi.connectionInfo) } catch (_: SecurityException) { }
                if (latestCells.isNotEmpty()) showCells(latestCells)
                if (pending && SystemClock.elapsedRealtime() - cellPoll > 15000) pending = false
                if (!pending && SystemClock.elapsedRealtime() - cellPoll > 5000) {
                    cellPoll = SystemClock.elapsedRealtime()
                    val manager = context.getSystemService(TelephonyManager::class.java)
                    val subscription = SubscriptionManager.getDefaultDataSubscriptionId()
                    val tm = if (SubscriptionManager.isValidSubscriptionId(subscription)) manager.createForSubscriptionId(subscription) else manager
                    try {
                        pending = true
                        tm.requestCellInfoUpdate(context.mainExecutor, object : TelephonyManager.CellInfoCallback() {
                            override fun onCellInfo(cells: MutableList<CellInfo>) { pending = false; latestCells = cells.toList(); showCells(cells) }
                            override fun onError(errorCode: Int, detail: Throwable?) { pending = false; if (!closed) { cellText = "Сота: обновление недоступно (код $errorCode)"; display(wifiText, cellText) } }
                        })
                    } catch (_: Exception) { pending = false; cellText = "Сота: недоступна на этом устройстве"; display(wifiText, cellText) }
                }
            }
            handler.postDelayed(this, 1000)
        }
    }
    fun close() { closed = true; handler.removeCallbacksAndMessages(null); callback?.let { cm.unregisterNetworkCallback(it) } }
}
