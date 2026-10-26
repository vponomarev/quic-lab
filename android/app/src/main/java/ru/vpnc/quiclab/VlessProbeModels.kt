package ru.vpnc.quiclab
import android.content.Context
import org.json.JSONObject
import org.json.JSONArray
import java.security.MessageDigest
internal data class ProbeNode(val id:String,val name:String,val endpoint:String,val config:JSONObject,val gateway:JSONObject?=null,val role:String="",val sourceName:String=""){
 fun publicRow()=JSONObject().put("id",id).put("name",name).put("endpoint",endpoint).put("serverName",config.optString("ServerName")).put("transport",config.optString("Transport").ifBlank{"tcp"}).put("sourceName",sourceName).put("role",role).put("haConfigured",gateway!=null).put("outcome","queued").put("relayOutcome",if(gateway==null)"not_applicable" else "").put("echoSucceeded",0).put("totalMs",0)
}
internal data class ProbeSource(val id:String,val name:String,val nodes:List<ProbeNode>){
 val fingerprint:String=probeHash(nodes.map{it.id+":"+it.config.toString()+":"+(it.gateway?.toString()?:"")}.sorted().joinToString("\n"))
 fun publicJSON()=JSONObject().put("id",id).put("name",name.take(100)).put("fingerprint",fingerprint)
}
internal fun probeHash(s:String)=MessageDigest.getInstance("SHA-256").digest(s.toByteArray()).joinToString(""){"%02x".format(it)}
internal const val ALL_PROBE_SOURCE_ID="__all_vless_sources__"
internal fun findProbeSource(sources:List<ProbeSource>,id:String):ProbeSource? =
 if(id==ALL_PROBE_SOURCE_ID)allProbeSource(sources)else sources.firstOrNull{it.id==id}
internal fun allProbeSource(sources:List<ProbeSource>):ProbeSource?{
 val nodes=sources.flatMap{source->source.nodes.map{node->node.copy(id=probeHash(source.id+":"+node.id),sourceName=source.name)}}
 if(nodes.isEmpty())return null
 return ProbeSource(ALL_PROBE_SOURCE_ID,"Все VLESS-источники",nodes)
}
internal object VlessProbeSources{
 fun list(c:Context):List<ProbeSource> = synchronized(MdmConfiguration.lock){
  val profiles=VpnProfiles.list(c);val sources=mutableListOf<ProbeSource>()
  for(p in profiles)runCatching{
   val prefs=VpnConfiguration.effectivePreferences(c,p.id);val identity=VpnIdentity.load(c,p.id)
   if(!identity.has("vless_config")&&!identity.has("vless_uris"))return@runCatching
   var gateway:JSONObject?=null
   if(!prefs.getString("relay_exit_profile_id","").isNullOrBlank()){
    val projected=VpnConfiguration.attachRuntime(c,p.id,JSONObject(identity.toString())).getJSONObject("_exit_runtime").getJSONObject("gateways")
    gateway=JSONObject(projected.getJSONObject(projected.keys().next()).toString());gateway!!.remove("underlay_vless_config");gateway!!.remove("underlay_endpoint")
   }
   val uris=identity.optJSONArray("vless_uris")
   val canonical=if(uris!=null)(0 until uris.length()).map{mobile.Mobile.importVLESSConfig(uris.getString(it))}else listOf(identity.getString("vless_config"))
   val status=VlessRelayStatus.snapshot()?.takeIf{it.optString("exitId")==p.id}?.optJSONArray("paths")?:JSONArray()
   val nodes=canonical.distinct().mapIndexed{i,raw->val config=JSONObject(raw).getJSONObject("config");val name=if(uris!=null)runCatching{VlessImport.metadata(uris.getString(i)).optString("name")}.getOrDefault("")else p.name
    val path=(0 until status.length()).map{status.getJSONObject(it)}.firstOrNull{it.optInt("index")==i+1}
    ProbeNode(probeHash(raw),name.ifBlank{config.getString("Endpoint")}.take(100),config.getString("Endpoint"),config,gateway,if(path==null)"" else if(path.optBoolean("primary"))"primary" else "reserve")}
   sources.add(ProbeSource(p.id,p.name,nodes))
  }
  // Aggregate separately imported entries of a subscription using the original deterministic IDs.
  val subs=VlessSubscriptions.list(c)
  val membership=if(subs.length()>0)VlessSubscriptions.membershipSnapshot(c)else JSONObject().put("profiles",JSONArray())
  for(id in subs.keys()){
   val sub=subs.getJSONObject(id);if(sub.optBoolean("asGroup"))continue
   val selected=sub.optJSONArray("selected")?:continue
   val ids=VlessSubscriptions.subscriptionProfileIds(membership,id,(0 until selected.length()).map{selected.getString(it)}.toSet())
   val members=sources.filter{it.id in ids};if(members.isNotEmpty()){sources.removeAll(members.toSet());sources.add(ProbeSource(id,sub.optString("name","Подписка"),members.flatMap{it.nodes}.distinctBy{it.id}))}
  }
  sources.filter{it.nodes.size in 1..256}.take(32)
 }
}
