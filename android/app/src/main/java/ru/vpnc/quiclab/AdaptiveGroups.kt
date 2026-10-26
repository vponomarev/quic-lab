package ru.vpnc.quiclab

import android.content.Context
import org.json.JSONArray
import org.json.JSONObject

/** Root profile owns routing and mode; members contribute isolated transport credentials. */
internal object AdaptiveGroups {
 const val MEMBERS="automatic_group_members"
 const val CANDIDATES="_group_candidates"
 val SUPPORTED=setOf("vless","awg","quic","https")
 private val connectionKeys=listOf("transport","endpoint","hostname","certificate","key","ca","server_name","verify_name","control_url","transit_endpoint","data_version","awg_config","vless_config")
 fun gateway(candidate:JSONObject,root:JSONObject):JSONObject {
  val selected=candidate.optJSONObject("gateway") ?: vlessGateway(candidate.getString("config"))
  return JSONObject(root.toString()).apply {
   for(key in connectionKeys+listOf("vless_uris","server_config","_exit_runtime",CANDIDATES,"underlay_vless_config","underlay_awg_config","underlay_endpoint"))remove(key)
   for(key in connectionKeys)if(selected.has(key))put(key,selected.get(key))
   put("max_availability",false)
  }
 }
 private fun vlessGateway(canonical:String)=JSONObject().put("transport","vless").put("vless_config",canonical)
  .put("endpoint",JSONObject(canonical).getJSONObject("config").getString("Endpoint"))
 internal fun logicalConnections(snapshot:JSONObject,subscriptions:JSONObject):Map<String,String> {
  val result=mutableMapOf<String,String>()
  val profiles=snapshot.getJSONArray("profiles")
  val present=(0 until profiles.length()).map{profiles.getJSONObject(it).getString("id")}.toSet()
  for(group in subscriptions.keys()){
   val selected=subscriptions.getJSONObject(group).optJSONArray("selected") ?: JSONArray()
   val keys=(0 until selected.length()).map{selected.getString(it)}.toSet()+VlessSubscriptions.profileKeys(snapshot,group).values
   val ids=VlessSubscriptions.subscriptionProfileIds(snapshot,group,keys).toMutableSet()
   val grouped=java.util.UUID.nameUUIDFromBytes(("vless-group:"+group).toByteArray()).toString()
   if(grouped in present)ids.add(grouped)
   for(id in ids){
    val logical="subscription.$group"
    require(result[id]==null || result[id]==logical){"Неоднозначная принадлежность профиля подписке"}
    result[id]=logical
   }
  }
  return result
 }
 fun candidates(id:String, source:JSONObject):List<JSONObject> {
  source.optJSONArray(CANDIDATES)?.let { a -> return (0 until a.length()).map { a.getJSONObject(it) } }
  val transport=source.optString("transport","vless")
  require(transport in SUPPORTED){"Транспорт не поддерживает автоматическую группу"}
  if(transport!="vless"){
   val selected=JSONObject().put("transport",transport)
   if(transport=="awg"){
    val raw=source.getString("awg_config");val info=AwgImport.metadata(raw)
    selected.put("awg_config",raw).put("endpoint",source.optString("endpoint").ifBlank{info.getString("endpoint")})
   }else{
    for(key in connectionKeys)if(source.has(key))selected.put(key,source.get(key))
    selected.remove("awg_config");selected.remove("vless_config")
    for(key in listOf("endpoint","certificate","key"))require(selected.optString(key).isNotBlank()){"Нет адреса или ключей подключения"}
   }
   return listOf(JSONObject().put("connection_id",id).put("transport",transport).put("config",selected.toString()).put("gateway",selected))
  }
  val uris=source.optJSONArray("vless_uris")
  val configs=if(uris!=null)(0 until uris.length()).map{mobile.Mobile.importVLESSConfig(uris.getString(it))}
   else listOf(source.optString("vless_config")).filter{it.isNotBlank()}
  require(configs.size in 1..256){"Подключение должно содержать от 1 до 256 VLESS узлов"}
  return configs.distinct().map{JSONObject().put("connection_id",id).put("config",it).put("transport","vless").put("gateway",vlessGateway(it))}
 }
 fun attach(context:Context,id:String,config:JSONObject) {
  val prefs=VpnConfiguration.effectivePreferences(context,id)
  val members=prefs.getStringSet(MEMBERS,emptySet()).orEmpty()
  if(members.isEmpty())return
  require(prefs.getString("transport","") in SUPPORTED){"Транспорт не поддерживает автоматическую группу"}
  require(members.size<=31 && id !in members){"Не более 32 профилей в группе; ссылка на себя запрещена"}
  val all=VpnProfiles.list(context).associateBy{it.id}
  val owners=listOf(id)+members.sorted()
  val logical=logicalConnections(VlessSubscriptions.membershipSnapshot(context),VlessSubscriptions.list(context))
  require(owners.map{logical[it] ?: it}.distinct().size<=16){"Не более 16 логических подключений в группе"}
  if(VpnProfiles.multiple(context)){
   val enabled=VpnProfiles.enabled(context)
   require(members.none{it in enabled}){"Участник группы не может одновременно быть отдельным выходом multiple"}
   for(other in enabled-id){
    val otherMembers=VpnConfiguration.effectivePreferences(context,other).getStringSet(MEMBERS,emptySet()).orEmpty()
    require(owners.none{it==other || it in otherMembers}){"Группы multiple имеют общие подключения"}
   }
  }
  val pools=owners.map { member ->
   require(member in all && MdmConfiguration.canSelectProfile(context,member)){"Участник группы отсутствует или запрещён MDM"}
   val p=VpnConfiguration.effectivePreferences(context,member)
   require(p.getString("transport","") in SUPPORTED){"Транспорт участника не поддерживает автоматическую группу"}
   require(member==id || p.getStringSet(MEMBERS,emptySet()).isNullOrEmpty()){"Вложенные группы не поддерживаются"}
   val source=if(member==id)JSONObject(config.toString()) else VpnIdentity.load(context,member)
   source.put("transport",p.getString("transport","vless"))
   for(key in listOf("endpoint","hostname","ca","server_name","verify_name","control_url","transit_endpoint"))if(p.contains(key))source.put(key,p.getString(key,""))
   if(p.contains("data_version"))source.put("data_version",p.getInt("data_version",1))
   candidates(logical[member] ?: member,source)
  }
  // Round-robin across connections prevents a large subscription monopolizing retries.
  val ordered=JSONArray()
  val seen=mutableSetOf<Pair<String,String>>()
  for(index in 0 until pools.maxOf{it.size})for(pool in pools)if(index<pool.size){
   val candidate=pool[index]
   if(seen.add(candidate.getString("connection_id") to candidate.getString("config")))ordered.put(candidate)
  }
  require(ordered.length()<=4096){"Не более 4096 узлов в группе; состав не применён"}
  config.put(CANDIDATES,ordered)
 }
}
