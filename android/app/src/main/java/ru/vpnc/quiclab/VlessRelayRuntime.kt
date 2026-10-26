package ru.vpnc.quiclab

import org.json.JSONArray
import org.json.JSONObject
import java.security.MessageDigest

/** Private runtime envelope: candidates carry ONE authenticated gateway session. */
internal object VlessRelayRuntime {
 fun resolve(runtime:JSONObject,previous:JSONObject=JSONObject(),resolver:(String)->String):JSONObject {
  val result=JSONObject(previous.toString());val gateways=runtime.getJSONObject("gateways")
  for(id in gateways.keys()) if(!result.has(id)) {
   val gateway=gateways.getJSONObject(id)
   val outer=gateway.optString("underlay_endpoint").ifBlank{gateway.optString("endpoint")}
   if(outer.isNotBlank()) try {result.put(id,resolver(outer))} catch(_:java.net.UnknownHostException) { /* Retry on this physical network later. */ }
  }
  return result
 }
 fun project(id:String,targetId:String,source:JSONObject,target:JSONObject):JSONObject {
  require(id!=targetId) {"Профиль общего выхода не может ссылаться на себя"}
  for(key in listOf("endpoint","certificate","key")) require(target.optString(key).isNotBlank()) {"Нет HTTPS адреса или ключей общего выхода"}
  val targets=JSONObject().put("https",target.optString("https_endpoint").ifBlank{target.getString("endpoint")})
  if(target.optString("quic_endpoint").isNotBlank())targets.put("quic",target.getString("quic_endpoint"))
  val candidates=AdaptiveGroups.candidates(id,source)
  require(candidates.isNotEmpty() && candidates.size<=4096) {"Нет допустимых путей"}
  val profiles=JSONArray();val gateways=JSONObject();val connections=JSONObject()
  candidates.distinctBy{it.getString("connection_id")+":"+it.getString("config")}.forEachIndexed {index,candidate->
   val original=AdaptiveGroups.gateway(candidate,source)
   val transport=original.getString("transport")
   val underlay=transport in setOf("vless","awg")
   val carrier=if(underlay)"https" else transport
   val endpoint=targets.optString(carrier)
   require(endpoint.isNotBlank()){"Общий выход не поддерживает транспорт $carrier"}
   if(!underlay) {
    require(original.optString("endpoint")==endpoint && listOf("certificate","key").all{original.optString(it)==target.optString(it)}){"Прямой путь ведёт к другому выходу"}
   }
   val canonical=candidate.getString("config")
   val connection=candidate.getString("connection_id")
   val hash=MessageDigest.getInstance("SHA-256").digest((transport+canonical).toByteArray(Charsets.UTF_8)).joinToString(""){"%02x".format(it)}
   val candidateId="$connection.${hash.take(32)}"
   connections.put(candidateId,connection)
   profiles.put(JSONObject().put("id",candidateId).put("exit_id",id).put("transport",carrier)
    .put("endpoint",endpoint).put("mode","auto").put("pool_size",1).put("priority",index))
   gateways.put(candidateId,JSONObject(original.toString()).apply {
    remove("vless_config");remove("awg_config")
    for(key in listOf("hostname","certificate","key","ca","server_name","verify_name","control_url","data_version","transit_endpoint")) {
     remove(key);if(target.has(key))put(key,target.get(key))
    }
    put("endpoint",endpoint)
    if(optString("hostname").isBlank())put("hostname",target.getString("endpoint").substringBeforeLast(':'))
    put("transport",carrier);put("max_availability",true)
    if(underlay){
     put("underlay_${transport}_config",original.getString("${transport}_config"))
     put("underlay_endpoint",original.getString("endpoint"))
    }
   })
  }
  val model=JSONObject().put("version",1).put("exits",JSONArray().put(JSONObject()
   .put("id",id).put("name","Общий выход").put("kind","demux").put("demux_id","relay:$targetId"))).put("profiles",profiles)
  mobile.Mobile.validateVpnConfiguration(model.toString())
  return JSONObject(source.toString()).put("transport","https").put("max_availability",true)
   .put("_exit_runtime",JSONObject().put("model",model).put("gateways",gateways).put("logical_connections",connections).put("target_endpoints",targets).put("economy",false))
 }
}
