package ru.vpnc.quiclab

import android.content.Context

internal object VpnBudgetSettings {
 fun preferences(context:Context):android.content.SharedPreferences {
  if(!MdmConfiguration.hasLayer(context)){
   val legacy=context.getSharedPreferences("vpn_budget",0)
   if(!legacy.contains("cell_mib"))legacy.edit().putLong("cell_mib",
    VpnProfiles.preferences(context).getLong("bond_cell_mib",0).coerceIn(0,1048576)).apply()
  }
  return MdmConfiguration.preferences(context,"vpn_budget")
 }
 fun limitBytes(context:Context):Long {
  val p=preferences(context)
  return if(p.contains("limit_bytes"))p.getLong("limit_bytes",0) else p.getLong("cell_mib",0).coerceIn(0,1048576)*1024*1024
 }
}
