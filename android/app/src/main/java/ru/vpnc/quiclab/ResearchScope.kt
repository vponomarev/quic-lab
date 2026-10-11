package ru.vpnc.quiclab

import org.json.JSONArray
import org.json.JSONObject

/** Independent of VPN application routing. Package names survive temporary uninstall. */
internal data class ResearchScope(val mode:String="vpn_only",val packages:Set<String> = emptySet()) {
 init {
  require(mode in setOf("vpn_only","vpn_plus_apps","all_apps")){"Неизвестный режим исследования"}
  require(packages.size<=2048 && packages.all{it.isNotBlank() && it.length<=255 && !it.contains('\u0000')})
 }
 fun json()=JSONObject().put("mode",mode).put("packages",JSONArray(packages.sorted()))
 fun vpnOnly()=copy(mode="vpn_only")
 companion object {
  fun parse(j:JSONObject?):ResearchScope {
   if(j==null)return ResearchScope()
   val p=j.optJSONArray("packages")?:JSONArray()
   require(p.length()<=2048)
   return ResearchScope(j.optString("mode","vpn_only"),(0 until p.length()).map{p.getString(it)}.toSet())
  }
 }
}
