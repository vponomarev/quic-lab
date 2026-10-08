package ru.vpnc.quiclab

import android.content.Context
import android.os.PowerManager

/** Device-wide diagnostics policy, shared by single and multiple VPN profiles. */
internal object VpnRttSettings {
    val values = longArrayOf(0, 1000, 5000)
    val labels = arrayOf("Выключено", "Каждую секунду", "Каждые 5 секунд")
    fun preferences(context: Context) = MdmConfiguration.preferences(context,"vpn_rtt")
    fun interval(context: Context): Long {
        val on = context.getSystemService(PowerManager::class.java).isInteractive
        val value = preferences(context).getLong(if (on) "screen_on" else "screen_off", if (on) 1000 else 0)
        return if (value in values) value else if (on) 1000 else 0
    }
}
