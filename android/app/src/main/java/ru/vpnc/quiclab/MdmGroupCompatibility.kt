package ru.vpnc.quiclab

import org.json.JSONArray
import org.json.JSONObject

internal object MdmGroupCompatibility {
 const val CAPABILITY="automatic_groups_v1"
 const val SERVER_FLAG="server_automatic_groups_v1"
 fun forServer(document:JSONObject,supported:Boolean):JSONObject? {
  val doc=JSONObject(document.toString())
  if(!supported){
   val profiles=doc.getJSONArray("profiles")
   for(i in 0 until profiles.length()){
    val settings=profiles.getJSONObject(i).getJSONObject("settings")
    // Legacy servers only understand VLESS relay roots. Do not publish a false
    // standalone replacement of a mixed HA configuration merely to pass validation.
    if(settings.optString("transport")!="vless" && settings.optString("relay_exit_profile_id").isNotBlank())return null
    settings.remove(AdaptiveGroups.MEMBERS)
   }
  }
  return doc
 }
 /** Old servers cannot express removing membership. Preserve it only in current mode;
  * external configurations remain independent and personal state is restored on detach. */
 fun incoming(desired:MdmConfigRevision,previous:JSONObject,supported:Boolean):MdmConfigRevision {
  if(supported)return desired
  val doc=JSONObject(desired.document.toString());val profiles=doc.getJSONArray("profiles")
  val old=previous.getJSONArray("profiles").let{a->(0 until a.length()).associate{a.getJSONObject(it).let{p->p.getString("id") to p.getJSONObject("settings")}}}
  for(i in 0 until profiles.length()){
   val p=profiles.getJSONObject(i);val settings=p.getJSONObject("settings")
   require(!settings.has(AdaptiveGroups.MEMBERS)){"Обновите MDM-сервер для управления группами"}
   if(desired.mode=="current")old[p.getString("id")]?.optJSONArray(AdaptiveGroups.MEMBERS)?.let{settings.put(AdaptiveGroups.MEMBERS,JSONArray(it.toString()))}
  }
  mobile.Mobile.validateMDMConfiguration(doc.toString())
  return desired.copy(document=doc)
 }
 fun allowsDependency(runtime:JSONObject,root:String,id:String):Boolean {
  val prefs=runtime.getJSONObject("preferences");val meta=prefs.getJSONObject("vpn_profiles")
  val enabled=meta.optJSONArray("multiple_enabled")?:JSONArray()
  val active=if(meta.optBoolean("multiple"))(0 until enabled.length()).any{enabled.optString(it)==root} else meta.optString("current")==root
  if(!active && !MdmProfileSelection.allows(runtime,root))return false
  val settings=prefs.optJSONObject(if(root=="default")"vpn" else "vpn_$root")?:return false
  return id==root || id in ConfigurationReferences.dependencies(settings)
 }
}
