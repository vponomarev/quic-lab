package ru.vpnc.quiclab

import org.json.JSONArray
import org.json.JSONObject
import java.security.MessageDigest

/** Private runtime envelope: candidates are carriers of ONE authenticated gateway session. */
internal object VlessRelayRuntime {
 /** Resolve only outer servers; never send the inner gateway name to local DNS. */
 fun resolve(runtime:JSONObject,previous:JSONObject=JSONObject(),resolver:(String)->String):JSONObject {
  val result=JSONObject(previous.toString());val gateways=runtime.getJSONObject("gateways")
  for(id in gateways.keys()) if(!result.has(id)) {
   val outer=gateways.getJSONObject(id).optString("underlay_endpoint")
   if(outer.isNotBlank()) try {result.put(id,resolver(outer))} catch(_:java.net.UnknownHostException) { /* Retry this candidate later on the same physical network. */ }
  }
  return result
 }
 fun project(id:String,targetId:String,source:JSONObject,target:JSONObject):JSONObject {
  require(id!=targetId) {"Профиль общего выхода не может ссылаться на себя"}
  for(key in listOf("endpoint","certificate","key")) require(target.optString(key).isNotBlank()) {"Нет HTTPS адреса или ключей общего выхода"}
  val uris=source.optJSONArray("vless_uris")
  val candidates=if(uris!=null) (0 until uris.length()).map {mobile.Mobile.importVLESSConfig(uris.getString(it))} else listOf(source.optString("vless_config")).filter{it.isNotBlank()}
  require(candidates.isNotEmpty() && candidates.size<=256) {"Нет допустимых VLESS путей"}
  val profiles=JSONArray();val gateways=JSONObject()
  candidates.distinct().forEachIndexed {index,canonical->
   val outer=JSONObject(canonical).getJSONObject("config").getString("Endpoint")
   require(outer.isNotBlank()) {"Нет адреса VLESS пути"}
   val hash=MessageDigest.getInstance("SHA-256").digest(canonical.toByteArray(Charsets.UTF_8)).joinToString(""){"%02x".format(it)}
   val candidateId="$id.${hash.take(32)}"
   profiles.put(JSONObject().put("id",candidateId).put("exit_id",id).put("transport","https")
    .put("endpoint",target.getString("endpoint")).put("mode","auto").put("pool_size",1).put("priority",index))
   gateways.put(candidateId,JSONObject(source.toString()).apply {
    remove("vless_uris");remove("vless_config");remove("_exit_runtime")
    for(key in listOf("endpoint","hostname","certificate","key","ca","server_name","verify_name","control_url","data_version","transit_endpoint")) {
     remove(key);if(target.has(key))put(key,target.get(key))
    }
    if(optString("hostname").isBlank())put("hostname",target.getString("endpoint").substringBeforeLast(':'))
    put("transport","https");put("max_availability",true)
    put("underlay_vless_config",canonical);put("underlay_endpoint",outer)
   })
  }
  val model=JSONObject().put("version",1).put("exits",JSONArray().put(JSONObject()
   .put("id",id).put("name","Общий выход").put("kind","demux").put("demux_id","relay:$targetId"))).put("profiles",profiles)
  mobile.Mobile.validateVpnConfiguration(model.toString())
  return JSONObject(source.toString()).put("transport","https").put("max_availability",true)
   .put("_exit_runtime",JSONObject().put("model",model).put("gateways",gateways).put("economy",false))
 }
}
