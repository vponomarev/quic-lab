package ru.vpnc.quiclab

import android.content.Context
import android.net.NetworkCapabilities
import android.os.PowerManager

/** Independent of RTT, global to all VPN profiles. Unknown Wi-Fi cost is treated as metered. */
internal object VpnReserveSettings {
    fun preferences(context: Context) = MdmConfiguration.preferences(context,"vpn_reserve")
    fun allowed(context: Context, kind: Int, unmetered: Boolean): Boolean {
        val on = context.getSystemService(PowerManager::class.java).isInteractive
        val wifi = kind == NetworkCapabilities.TRANSPORT_WIFI
        val prefs = preferences(context)
        val key = (if (wifi) "wifi_" else "cell_") + (if (on) "on" else "off")
        return permits(wifi, unmetered, prefs.getBoolean(key, wifi), prefs.getBoolean("metered_wifi", false))
    }
    fun permits(wifi: Boolean, unmetered: Boolean, enabled: Boolean, allowMeteredWifi: Boolean) =
        enabled && (!wifi || unmetered || allowMeteredWifi)
}
