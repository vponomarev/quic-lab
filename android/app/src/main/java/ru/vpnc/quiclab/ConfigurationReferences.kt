package ru.vpnc.quiclab

import org.json.JSONObject
import org.json.JSONArray

internal object ConfigurationReferences {
 fun dependencies(settings:JSONObject):Set<String> {
  val ids=linkedSetOf<String>()
  settings.optJSONArray(AdaptiveGroups.MEMBERS)?.let{a->for(i in 0 until a.length())ids.add(a.getString(i))}
  settings.optString("relay_exit_profile_id").takeIf{it.isNotBlank()}?.let{ids.add(it)}
  return ids
 }
 fun names(payload:JSONObject):Map<String,String> {
  val names=linkedMapOf<String,String>()
  val profiles=payload.optJSONArray("profiles")?:JSONArray()
  for(i in 0 until profiles.length())profiles.getJSONObject(i).let{names[it.getString("id")]=it.getString("name")}
  for(i in 0 until profiles.length())for(id in dependencies(profiles.getJSONObject(i).getJSONObject("settings")))names.putIfAbsent(id,id)
  payload.optJSONObject("routing")?.let{r->
   names.putIfAbsent(r.getString("currentProfileId"),r.getString("currentProfileId"))
   r.getJSONObject("settings").keys().forEach{names.putIfAbsent(it,it)}
   val ids=r.getJSONArray("enabledProfileIds");for(i in 0 until ids.length())names.putIfAbsent(ids.getString(i),ids.getString(i))
  }
  payload.optJSONObject("network")?.getJSONObject("dns")?.getString("profileId")?.let{names.putIfAbsent(it,it)}
  return names
 }
 fun incomplete(document:JSONObject,missing:Set<String>):Boolean {
  val profiles=document.getJSONArray("profiles")
  val settings=(0 until profiles.length()).associate{profiles.getJSONObject(it).let{p->p.getString("id") to p.getJSONObject("settings")}}
  val roots=if(document.getBoolean("multiple"))document.getJSONArray("enabledProfileIds").let{a->(0 until a.length()).map{a.getString(it)}} else listOf(document.getString("currentProfileId"))
  val pending=ArrayDeque(roots)
  val dns=document.getJSONObject("dns");if(dns.getString("mode")=="tunnel")pending.add(dns.getString("profileId"))
  val seen=mutableSetOf<String>()
  while(pending.isNotEmpty()){
   val id=pending.removeFirst();if(!seen.add(id))continue
   if(id in missing || id !in settings)return true
   pending.addAll(dependencies(settings.getValue(id)))
  }
  return false
 }
 /** Include retained profiles, not only new IDs reported by the native importer. */
 fun missingCredentials(document:JSONObject,runtime:JSONObject):Set<String> {
  val identities=runtime.getJSONObject("identities")
  val profiles=document.getJSONArray("profiles")
  return (0 until profiles.length()).map{profiles.getJSONObject(it)}.filter{profile->
   val identity=identities.optJSONObject(profile.getString("id"))?:JSONObject()
   val required=when(profile.getJSONObject("settings").optString("transport","quic")){
    "vless"->listOf("vless_config")
    "awg"->listOf("awg_config")
    else->listOf("certificate","key")
   }
   required.any{identity.optString(it).isBlank()}
  }.map{it.getString("id")}.toSet()
 }
 /** Validate against the destination runtime: subscription affinity is local metadata. */
 fun validateGroups(document:JSONObject,runtime:JSONObject) {
  val profiles=document.getJSONArray("profiles")
  if((0 until profiles.length()).none{profiles.getJSONObject(it).getJSONObject("settings").optJSONArray(AdaptiveGroups.MEMBERS)?.length()?.let{n->n>0}==true})return
  val identities=runtime.getJSONObject("identities")
  val snapshot=VlessSubscriptions.membershipSnapshot(identities.keys().asSequence().associateWith{identities.getJSONObject(it)})
  val logical=AdaptiveGroups.logicalConnections(snapshot,runtime.optJSONObject("subscriptions")?:JSONObject())
  for(i in 0 until profiles.length()){
   val profile=profiles.getJSONObject(i)
   val members=profile.getJSONObject("settings").optJSONArray(AdaptiveGroups.MEMBERS)?:continue
   if(members.length()==0)continue
   val owners=listOf(profile.getString("id"))+(0 until members.length()).map{members.getString(it)}
   require(owners.map{logical[it]?:it}.distinct().size<=16){
    "${profile.getString("name")}: после переноса больше 16 логических подключений. Объедините узлы подписки в один профиль группы перед переносом."
   }
  }
 }
}
