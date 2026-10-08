package ru.vpnc.quiclab

import android.content.Context
import android.content.SharedPreferences
import org.json.JSONObject
import org.json.JSONArray

/** Sole preference/identity authority after the first managed configuration.
 * Legacy files remain personal migration inputs only; external data never enters them.
 */
internal object MdmConfiguration {
 internal val lock=Any()
 private fun store(c:Context)=MdmConfigurationStore(c)
 fun managed(c:Context)=state(c).let{it.active && it.rights.config}
 fun hasLayer(c:Context)=store(c).runtime()!=null
 fun serverOverlay(c:Context,id:String):Boolean {
  val r=store(c).runtime()?:return true
  val profiles=r.optJSONArray("overlayProfiles")
  return r.optBoolean("serverOverlay",true) || profiles!=null && (0 until profiles.length()).any{profiles.getString(it)==id}
 }
 private fun state(c:Context)=MdmStore(c).read()
 internal fun token(c:Context)=synchronized(lock){state(c).localGeneration to store(c).configEpoch()}
 internal fun assertEditable(c:Context,expected:Pair<Long,Long>?=null){
  val s=state(c)
  check(!s.cleanupPending && !(s.active && s.rights.config)){"Настройками управляет MDM. Приостановите управление для редактирования."}
  if(expected!=null)check(expected==token(c)){"Настройки изменились. Откройте редактор заново."}
 }
 fun preferences(c:Context,name:String):SharedPreferences=ConfigurationPreferences(c,name)
 internal fun values(c:Context,name:String):Map<String,*> {
  val r=store(c).runtime()?:return c.getSharedPreferences(name,0).all
  val obj=r.getJSONObject("preferences").optJSONObject(name)?:JSONObject()
  if(name=="diagnostics_settings"){
   val result=c.getSharedPreferences(name,0).all.toMutableMap()
   for(k in listOf("enabled","detailed"))if(obj.has(k))result[k]=obj.get(k)
   return result
  }
  return obj.keys().asSequence().associateWith{k->when(val v=obj.get(k)){
   is JSONArray->(0 until v.length()).map{v.getString(it)}.toSet()
   else->v
  }}
 }
 private fun encode(values:Map<String,*>):JSONObject=JSONObject().also{j->values.forEach{(k,v)->
  if(v!=null)j.put(k,if(v is Set<*>)JSONArray(v.filterIsInstance<String>().sorted())else v)
 }}
 internal fun edit(c:Context,name:String,expected:Pair<Long,Long>,clear:Boolean,edits:Map<String,Any?>):Boolean=synchronized(lock){
  val changedKeys=edits.keys + if(clear)values(c,name).keys else emptySet()
  val operational=name=="diagnostics_settings" && changedKeys.none{it=="enabled"||it=="detailed"}
  if(!operational)assertEditable(c,expected)
  if(!hasLayer(c) || operational){
   val e=c.getSharedPreferences(name,0).edit();if(clear)e.clear()
   edits.forEach{(k,v)->when(v){
    null->e.remove(k);is String->e.putString(k,v);is Boolean->e.putBoolean(k,v)
    is Int->e.putInt(k,v);is Long->e.putLong(k,v);is Float->e.putFloat(k,v)
    is Set<*>->e.putStringSet(k,v.filterIsInstance<String>().toSet())
    else->error("Unsupported preference type")
   }}
   val committed=e.commit();if(committed)ConfigurationPreferences.notify(c,name,changedKeys)
   return@synchronized committed
  }
  store(c).editRuntime{r->
   val all=r.getJSONObject("preferences")
   val obj=if(clear)JSONObject()else all.optJSONObject(name)?:JSONObject()
   edits.forEach{(k,v)->if(v==null)obj.remove(k)else obj.put(k,if(v is Set<*>)JSONArray(v.filterIsInstance<String>().sorted())else v)}
   if(name=="vpn_budget" && "cell_mib" in edits)obj.remove("limit_bytes")
   all.put(name,obj)
  }
  ConfigurationPreferences.notify(c,name,changedKeys)
  true
 }
 fun identity(c:Context,id:String):JSONObject?=store(c).runtime()?.getJSONObject("identities")?.optJSONObject(id)
 fun writeIdentity(c:Context,id:String,bundle:JSONObject):Boolean=synchronized(lock){
  assertEditable(c)
  if(!hasLayer(c))return@synchronized false
  store(c).editRuntime{r->
   r.getJSONObject("identities").put(id,JSONObject(bundle.toString()))
   val existing=r.optJSONArray("overlayProfiles")?:JSONArray()
   val ids=(0 until existing.length()).map{existing.getString(it)}.toMutableSet()
   if(bundle.has("server_config"))ids.add(id) else ids.remove(id)
   r.put("overlayProfiles",JSONArray(ids.sorted()))
  };true
 }
 fun deleteIdentity(c:Context,id:String):Boolean=synchronized(lock){
  assertEditable(c);if(!hasLayer(c))return@synchronized false
  store(c).editRuntime{it.getJSONObject("identities").remove(id)};true
 }
 private val settingsKeys=setOf("transport","endpoint","quic_endpoint","https_endpoint","awg_endpoint","vless_endpoint",
  "hostname","dns","mode","routes","apps","global_apps","ca","server_name","verify_name","control_url","data_version",
  "transit_endpoint","max_availability","bond_copy_budget","bond_cell_budget","bond_copy_kib","cell_mib",
  "available_transports","demux_enabled","quic_mode","https_mode","quic_pool_size","https_pool_size","quic_check_reserve","https_check_reserve")
 fun snapshotLocal(c:Context):JSONObject {
  val profiles=VpnProfiles.list(c)
  val current=VpnProfiles.current(c).id
  val items=JSONArray()
  for(p in profiles){
   val settings=VpnProfiles.preferences(c,p.id).all.filterKeys{it in settingsKeys}
   items.put(JSONObject().put("id",p.id).put("name",p.name).put("settings",encode(settings)))
  }
  return JSONObject().put("schema",1).put("profiles",items).put("currentProfileId",current)
   .put("enabledProfileIds",JSONArray(VpnProfiles.enabled(c).sorted())).put("multiple",VpnProfiles.multiple(c))
   .put("globalApps",JSONArray(VpnProfiles.globalApps(c).sorted()))
   .put("dns",JSONObject().put("mode",VpnProfiles.dnsMode(c)).put("profileId",VpnProfiles.dnsProfile(c).ifEmpty{current}))
   .put("budget",JSONObject().put("limitBytes",VpnBudgetSettings.limitBytes(c)))
   .put("diagnostics",JSONObject().put("enabled",DiagnosticsPolicy.enabled(c)).put("detailed",DiagnosticsPolicy.detailed(c)))
 }
 private fun snapshotRuntime(c:Context):JSONObject {
  val prefs=JSONObject();val identities=JSONObject()
  for(name in listOf("vpn_profiles","vpn_budget","vpn_rtt","vpn_reserve","diagnostics_settings")){
   prefs.put(name,encode(values(c,name)))
  }
  for(p in VpnProfiles.list(c)){
   val name=if(p.id=="default")"vpn" else "vpn_${p.id}"
   prefs.put(name,encode(values(c,name)))
   if(VpnIdentity.exists(c,p.id))identities.put(p.id,VpnIdentity.load(c,p.id))
  }
  return JSONObject().put("preferences",prefs).put("identities",identities).put("serverOverlay",true)
 }
 /** The caller must stop the VPN first. Live transition orchestration is separate. */
 fun apply(c:Context,desired:MdmConfigRevision,generation:Long):Boolean=synchronized(lock){
  val s=state(c)
  check(s.active && !s.cleanupPending && s.rights.config && s.localGeneration==generation){"Нет действующего разрешения на конфигурацию MDM"}
  check(!LabVpnService.active){"Перед применением конфигурации остановите VPN"}
  mobile.Mobile.validateMDMConfiguration(desired.document.toString())
  val target=store(c)
  if(!hasLayer(c))target.initialize(snapshotLocal(c),snapshotRuntime(c))
  target.apply(s.binding!!.id,desired,::compile)
 }
 private fun compile(doc:JSONObject,previous:JSONObject):JSONObject {
  val oldPrefs=previous.getJSONObject("preferences")
  val oldKeys=previous.getJSONObject("identities")
  val prefs=JSONObject();val keys=JSONObject();val names=JSONArray()
  for(name in listOf("vpn_rtt","vpn_reserve"))prefs.put(name,JSONObject((oldPrefs.optJSONObject(name)?:JSONObject()).toString()))
  val items=doc.getJSONArray("profiles")
  for(i in 0 until items.length()){
   val p=items.getJSONObject(i);val id=p.getString("id")
   names.put(JSONObject().put("id",id).put("name",p.getString("name")))
   val settings=JSONObject(p.getJSONObject("settings").toString())
   prefs.put(if(id=="default")"vpn" else "vpn_$id",settings)
   val identity=p.optJSONObject("identity")
   if(identity!=null){
    val bundle=JSONObject().put("subject",p.getString("name"))
    for(k in listOf("certificate","key","awg_config"))if(identity.has(k))bundle.put(k,identity.getString(k))
    if(identity.has("vless_uri"))bundle.put("vless_config",mobile.Mobile.importVLESSConfig(identity.getString("vless_uri")))
    keys.put(id,bundle)
   }else if(!p.optBoolean("removeIdentity") && oldKeys.has(id))keys.put(id,JSONObject(oldKeys.getJSONObject(id).toString()))
  }
  val dns=doc.getJSONObject("dns")
  prefs.put("vpn_profiles",JSONObject().put("profiles",names.toString()).put("current",doc.getString("currentProfileId"))
   .put("multiple",doc.getBoolean("multiple")).put("multiple_enabled",doc.getJSONArray("enabledProfileIds"))
   .put("global_apps",doc.getJSONArray("globalApps")).put("dns_mode",dns.getString("mode")).put("multiple_dns",dns.getString("profileId")))
  prefs.put("vpn_budget",JSONObject().put("limit_bytes",doc.getJSONObject("budget").getLong("limitBytes")).put("cell_mib",doc.getJSONObject("budget").getLong("limitBytes")/(1024*1024)))
  prefs.put("diagnostics_settings",JSONObject(doc.getJSONObject("diagnostics").toString()))
  return JSONObject().put("preferences",prefs).put("identities",keys).put("serverOverlay",false)
 }
 fun detach(c:Context)=synchronized(lock){
  if(!hasLayer(c))return@synchronized
  check(store(c).mode()!="external" || !LabVpnService.active){"Сначала остановите VPN с внешним конфигом"}
  store(c).detach()
 }
}
