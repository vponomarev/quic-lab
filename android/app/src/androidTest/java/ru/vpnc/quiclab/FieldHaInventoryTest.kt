package ru.vpnc.quiclab

import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.json.JSONArray
import org.junit.Test

/** Read-only inventory for an explicitly requested device acceptance run. Never emits credentials. */
class FieldHaInventoryTest {
 @Test fun inventory() {
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val rows=JSONArray()
  for(profile in VpnProfiles.list(c)) {
   val p=VpnConfiguration.effectivePreferences(c,profile.id)
   val identity=runCatching{VpnIdentity.load(c,profile.id)}.getOrNull()
   rows.put(JSONObject().put("id",profile.id).put("name",profile.name)
    .put("transport",p.getString("transport","quic"))
    .put("selectable",MdmConfiguration.canSelectProfile(c,profile.id))
    .put("has_awg",!identity?.optString("awg_config").isNullOrBlank())
    .put("has_vless",!identity?.optString("vless_config").isNullOrBlank() || (identity?.optJSONArray("vless_uris")?.length() ?: 0)>0)
    .put("has_certificate",!identity?.optString("certificate").isNullOrBlank())
    .put("has_https",!p.getString("https_endpoint","").isNullOrBlank() || p.getString("transport","")=="https")
    .put("relay_target",p.getString("relay_exit_profile_id","")))
  }
  val reserve=JSONObject();VpnReserveSettings.preferences(c).all.forEach{(k,v)->reserve.put(k,v)}
  val cm=c.getSystemService(android.net.ConnectivityManager::class.java)
  val networks=JSONArray()
  for(n in cm.allNetworks){val caps=cm.getNetworkCapabilities(n)?:continue
   if(caps.hasTransport(android.net.NetworkCapabilities.TRANSPORT_VPN))continue
   networks.put(JSONObject().put("wifi",caps.hasTransport(android.net.NetworkCapabilities.TRANSPORT_WIFI)).put("cell",caps.hasTransport(android.net.NetworkCapabilities.TRANSPORT_CELLULAR)).put("validated",caps.hasCapability(android.net.NetworkCapabilities.NET_CAPABILITY_VALIDATED)))
  }
  println("FIELD_HA_INVENTORY "+JSONObject().put("profiles",rows).put("reserve",reserve).put("networks",networks)
   .put("current",VpnProfiles.current(c).id).put("multiple",VpnProfiles.multiple(c)).put("managed",MdmConfiguration.managed(c))
   .put("vpn_active",LabVpnService.active).put("consent_needed",android.net.VpnService.prepare(c)!=null)
   .put("wifi_enabled",c.getSystemService(android.net.wifi.WifiManager::class.java).isWifiEnabled))
 }
}
