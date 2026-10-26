package ru.vpnc.quiclab

import android.content.Context
import android.os.SystemClock
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/** Explicit field-only diagnostics. No endpoints, keys, subscription URL or exit IP in output. */
internal object FieldSubscriptionDiagnostics {
 fun freshIdentity(c:Context):JSONObject {
  val subscriptions=VlessSubscriptions.list(c)
  require(subscriptions.length()==1){"Expected one saved subscription"}
  val meta=subscriptions.getJSONObject(subscriptions.keys().next())
  val parsed=VlessSubscriptions.fetch(meta.getString("url"))
  return JSONObject().put("vless_uris",JSONArray(parsed.entries.map{it.uri}))
 }
 fun compareFresh(c:Context) {
  val old=AdaptiveGroups.candidates("subscription",identity(c).put("transport","vless")).map{it.getString("config")}.toSet()
  val fresh=freshIdentity(c)
  val current=AdaptiveGroups.candidates("subscription",JSONObject(fresh.toString()).put("transport","vless")).map{it.getString("config")}.toSet()
  File(c.filesDir,"field-ha-report.json").writeText(JSONObject().put("diagnostic","refresh").put("saved_count",old.size).put("fresh_count",current.size).put("unchanged",old==current).put("common",old.intersect(current).size).put("finished",true).toString())
 }
 fun identity(c:Context):JSONObject {
  val profiles=VpnProfiles.list(c).filter{VpnConfiguration.effectivePreferences(c,it.id).getString("transport","")=="vless"}
  require(profiles.size==1){"Expected one existing VLESS subscription profile"}
  return VpnIdentity.load(c,profiles.single().id)
 }
 fun run(c:Context,source:JSONObject,fresh:Boolean=false) {
  val bundle=(if(fresh)freshIdentity(c) else identity(c)).put("transport","vless")
  val configs=AdaptiveGroups.candidates("subscription",bundle).map{JSONObject(it.getString("config")).getJSONObject("config")}
  val rows=JSONArray()
  val report=JSONObject().put("diagnostic",true).put("fresh",fresh).put("node_count",configs.size).put("rows",rows).put("finished",false)
  if(fresh) {
   val saved=AdaptiveGroups.candidates("subscription",identity(c).put("transport","vless")).map{JSONObject(it.getString("config")).getJSONObject("config")}.associateBy{it.getString("Endpoint")}
   report.put("same_endpoint",configs.count{saved.containsKey(it.getString("Endpoint"))})
   for(key in listOf("UUID","RealityPublicKey","ShortID","ServerName","Flow","Fingerprint"))report.put("changed_$key",configs.count{n->saved[n.getString("Endpoint")]?.let{it.optString(key)!=n.optString(key)}?:false})
  }
  val file=File(c.filesDir,"field-ha-report.json")
  fun save(){file.writeText(report.toString())}
  save()
  val gateway=JSONObject().put("transport","https").put("endpoint",source.getString("https"))
   .put("hostname",source.getString("hostname")).put("certificate",source.getString("certificate")).put("key",source.getString("key")).put("ca",source.optString("ca"))
  for(kind in listOf("wifi","cell")) {
   VlessProbeNetwork.acquire(c,kind).use{lease->
    val workers=Executors.newFixedThreadPool(2)
    try {
     val futures=configs.mapIndexed{index,original->workers.submit{
      val row=JSONObject().put("index",index+1).put("network",kind).put("transport",original.optString("Transport")).put("security",original.optString("Security"))
      var probe:mobile.VLESSProbe?=null
      try {
       val config=JSONObject(original.toString());val endpoint=config.getString("Endpoint")
       val ip=lease.resolve(endpoint.substringBeforeLast(':').removeSurrounding("[","]"),SystemClock.elapsedRealtime()+10000)
       config.put("Endpoint",if(ip.contains(':'))"[$ip]:${endpoint.substringAfterLast(':')}" else "$ip:${endpoint.substringAfterLast(':')}")
       probe=mobile.Mobile.newVLESSProbe(config.toString(),lease.binder,lease.budget,kind)
       val options=JSONObject().put("target","https://"+source.getString("hostname")+"/api/v1/probe/echo").put("gateway",gateway)
       val result=JSONObject(probe!!.run(options.toString()))
       result.remove("exitIp");result.remove("exitIpProvider")
       row.put("result",result)
      }catch(e:Exception){row.put("error_class",e.javaClass.simpleName)}
      finally {probe?.close()}
      synchronized(report){rows.put(row);save()}
     }}
     futures.forEach{it.get()}
    } finally {workers.shutdownNow();workers.awaitTermination(30,TimeUnit.SECONDS)}
   }
  }
  report.put("finished",true);save()
 }
}
