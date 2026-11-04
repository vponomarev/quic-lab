package ru.vpnc.quiclab

import org.json.JSONArray
import org.json.JSONObject

/** Public wire projection, independent of local diagnostic/credential storage. */
internal object MdmProbeReport {
 const val CAPABILITY="vless_probe_phases_v2"
 const val SERVER_FLAG="server_vless_probe_phases_v2"
 private val header=listOf("runId","sourceId","fingerprint","sequence","network","initiator","state","startedAt","finishedAt")
 private val rowFields=listOf("id","name","endpoint","serverName","outcome","relayOutcome","echoSucceeded","stage","role","totalMs","echoRttMs","measuredAt","dnsOutcome","tcpOutcome","proxyOutcome","httpsOutcome","httpsStage","exitIpOutcome","exitIpStage","exitIp","exitIpProvider")
 private val integers=setOf("sequence","echoSucceeded","totalMs","echoRttMs")
 private fun copy(value:JSONObject,keys:List<String>)=JSONObject().also{out->keys.forEach{k->
  if(value.has(k)&&!value.isNull(k)){
   val field=value.get(k)
   require(if(k in integers)field is Int||field is Long else field is String)
   out.put(k,field)
  }
 }}
 private fun rows(value:JSONObject):JSONArray {
  val input=value.getJSONArray("rows");require(input.length() in 1..256)
  return JSONArray().also{out->for(i in 0 until input.length())out.put(copy(input.getJSONObject(i),rowFields))}
 }
 fun catalogue(sources:List<ProbeSource>,phasesSupported:Boolean):List<ProbeSource>{
  val aggregate=if(phasesSupported&&sources.sumOf{it.nodes.size} in 1..256)allProbeSource(sources)else null
  return if(aggregate==null)sources.take(32)else listOf(aggregate)+sources.take(31)
 }
 fun select(all:JSONObject,sources:List<ProbeSource>,state:MdmState,phasesSupported:Boolean,sequenceSupported:Boolean=false):JSONObject? {
  if(!state.active||state.cleanupPending||!state.rights.vpn||!state.rights.telemetry)return null
  return all.keys().asSequence().mapNotNull{all.optJSONObject(it)}.filter{run->
   VlessProbePolicy.owns(run,state)&&sources.any{it.id==run.optString("sourceId")&&it.fingerprint==run.optString("fingerprint")}
  }.sortedByDescending{it.optString("startedAt")}.mapNotNull{run->runCatching{
   val schema=run.optInt("schema",1);require(schema==1||schema==2&&phasesSupported&&(run.optString("initiator")=="local"||sequenceSupported&&run.optString("initiator")=="mdm"))
   val out=copy(run,header)
   if(schema==1){out.put("rows",rows(run))}else{
    val phases=run.getJSONObject("phases");require(phases.length() in 1..2)
    val public=JSONObject()
    for(kind in phases.keys()){
     require(kind in listOf("wifi","cell"));val phase=phases.getJSONObject(kind);require(phase.getString("network")==kind)
     public.put(kind,copy(phase,listOf("network","state","reason","startedAt","finishedAt")).put("rows",rows(phase)))
    }
    out.put("schema",2).put("network",run.optString("network","none")).put("phases",public)
   }
   require(out.toString().toByteArray(Charsets.UTF_8).size<=if(schema==2)768*1024 else 512*1024)
   out
  }.getOrNull()}.firstOrNull()
 }
 fun append(out:JSONObject,snapshot:JSONObject?){
  if(snapshot==null)return
  val candidate=JSONObject(out.toString()).put("probeSnapshots",JSONArray().put(snapshot))
  // Leave space for the enclosing request and keep the existing 1 MiB ceiling.
  if(candidate.toString().toByteArray(Charsets.UTF_8).size<=900*1024)out.put("probeSnapshots",candidate.getJSONArray("probeSnapshots"))
 }
}
