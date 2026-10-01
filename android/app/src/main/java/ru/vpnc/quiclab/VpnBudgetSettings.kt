package ru.vpnc.quiclab

import android.content.Context

/** One limit for all exits. Import the selected legacy profile once. */
internal object VpnBudgetSettings {
    fun preferences(context: Context) = context.getSharedPreferences("vpn_budget", Context.MODE_PRIVATE).also {
        if (!it.contains("cell_mib")) it.edit().putLong("cell_mib",
            VpnProfiles.preferences(context).getLong("bond_cell_mib", 0).coerceIn(0,1048576)).apply()
    }
    fun limitBytes(context: Context) = preferences(context).getLong("cell_mib",0).coerceIn(0,1048576)*1024*1024
}
