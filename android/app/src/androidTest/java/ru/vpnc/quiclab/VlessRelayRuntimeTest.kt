package ru.vpnc.quiclab
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Test
import org.junit.Assert.*
import org.junit.runner.RunWith
import org.json.JSONObject
import org.json.JSONArray
@RunWith(AndroidJUnit4::class)
class VlessRelayRuntimeTest {
 private fun uri(host:String)="vless://11111111-1111-4111-8111-111111111111@$host:443?security=tls&type=tcp&sni=example.org"
 private fun source()=JSONObject().put("transport","vless").put("vless_uris",JSONArray().put(uri("first.example")).put(uri("second.example")))
 private fun target()=JSONObject().put("endpoint","gateway.example:443").put("hostname","gateway.example").put("certificate","test-cert").put("key","test-key").put("ca","test-ca").put("control_url","https://gateway.example/control")
 @Test fun projectsOneExitAndIndependentUnderlays(){
  val s=source();val r=VlessRelayRuntime.project("group","gateway",s,target())
  assertEquals("https",r.getString("transport"));assertEquals("vless",s.getString("transport"))
  val envelope=r.getJSONObject("_exit_runtime");val model=envelope.getJSONObject("model")
  assertEquals(1,model.getJSONArray("exits").length())
  val profiles=model.getJSONArray("profiles");assertEquals(2,profiles.length())
  for(i in 0 until profiles.length()){
   val p=profiles.getJSONObject(i);val g=envelope.getJSONObject("gateways").getJSONObject(p.getString("id"))
   assertEquals("https",p.getString("transport"));assertEquals("group",p.getString("exit_id"))
   assertEquals(1,p.getInt("pool_size"));assertEquals("gateway.example:443",p.getString("endpoint"))
   assertEquals("test-cert",g.getString("certificate"));assertEquals("test-key",g.getString("key"))
   assertEquals("https://gateway.example/control",g.getString("control_url"))
   assertEquals(if(i==0)"first.example:443" else "second.example:443",g.getString("underlay_endpoint"))
   assertTrue(g.getString("underlay_vless_config").isNotEmpty())
  }
  assertFalse(model.toString().contains("test-key"));assertFalse(model.toString().contains("vless://"))
 }
 @Test fun stableCandidateIdentityAndDeduplication(){
  fun ids(s:JSONObject):List<String>{val a=VlessRelayRuntime.project("group","gateway",s,target()).getJSONObject("_exit_runtime").getJSONObject("model").getJSONArray("profiles");return (0 until a.length()).map{a.getJSONObject(it).getString("id")}}
  val original=ids(source());val reversed=source().put("vless_uris",JSONArray().put(uri("second.example")).put(uri("first.example")).put(uri("first.example")))
  assertEquals(original.reversed(),ids(reversed))
 }
 @Test fun rejectsAbsentGatewayIdentityAndEmptyCandidates(){
  for(key in listOf("certificate","key","endpoint")){
   val t=target();t.remove(key)
   assertTrue(runCatching{VlessRelayRuntime.project("group","gateway",source(),t)}.isFailure)
  }
  assertTrue(runCatching{VlessRelayRuntime.project("group","gateway",JSONObject(),target())}.isFailure)
  assertTrue(runCatching{VlessRelayRuntime.project("group","group",source(),target())}.isFailure)
 }
 @Test fun dnsOnlyResolvesUnderlaysAndLateAnswerPreservesExistingAddress(){
  val runtime=VlessRelayRuntime.project("group","gateway",source(),target()).getJSONObject("_exit_runtime")
  val called=mutableListOf<String>()
  val first=VlessRelayRuntime.resolve(runtime){address->called.add(address);if(address.startsWith("second"))throw java.net.UnknownHostException();"192.0.2.1:443"}
  assertEquals(1,first.length());assertEquals(setOf("first.example:443","second.example:443"),called.toSet())
  called.clear()
  val second=VlessRelayRuntime.resolve(runtime,first){address->called.add(address);"192.0.2.2:443"}
  assertEquals(2,second.length());assertEquals(listOf("second.example:443"),called)
  for(id in first.keys())assertEquals(first.getString(id),second.getString(id))
 }

 @Test fun statusContainsOnlyBoundedMetadata(){
  val runtime=VlessRelayRuntime.project("group","gateway",source(),target()).getJSONObject("_exit_runtime")
  val ps=runtime.getJSONObject("model").getJSONArray("profiles")
  val snapshot=JSONObject().put("running",true).put("generation",4).put("preferred","path-a")
   .put("paths",JSONArray().put(JSONObject().put("path_id","path-a").put("profile_id",ps.getJSONObject(0).getString("id")).put("network","wifi").put("ready",true))
    .put(JSONObject().put("path_id","path-b").put("profile_id",ps.getJSONObject(1).getString("id")).put("network","wifi").put("ready",true)))
  try {
   VlessRelayStatus.update(runtime,snapshot);val status=VlessRelayStatus.snapshot()!!
   assertEquals("ready",status.getString("state"));assertTrue(status.getJSONArray("paths").getJSONObject(0).getBoolean("primary"))
   assertFalse(status.toString().contains("test-key"));assertFalse(status.toString().contains("vless://"));assertFalse(status.toString().contains("example"))
   snapshot.getJSONArray("paths").remove(1);VlessRelayStatus.update(runtime,snapshot);assertEquals("degraded",VlessRelayStatus.snapshot()!!.getString("state"))
   VlessRelayStatus.update(runtime,snapshot,true);assertEquals("session_lost",VlessRelayStatus.snapshot()!!.getString("state"))
  }finally{VlessRelayStatus.clear()}
 }

 @Test fun innerHostnameFallbackDoesNotNeedLocalDNS(){
  val t=target();t.remove("hostname")
  val r=VlessRelayRuntime.project("group","gateway",source(),t).getJSONObject("_exit_runtime").getJSONObject("gateways")
  for(id in r.keys())assertEquals("gateway.example",r.getJSONObject(id).getString("hostname"))
 }
 @Test fun statusTracksBondSessionRatherThanRuntimeGeneration(){
  val runtime=VlessRelayRuntime.project("group","gateway",source(),target()).getJSONObject("_exit_runtime")
  val snapshot=JSONObject().put("running",true).put("generation",1).put("connection_id","bond-41").put("paths",JSONArray())
  try{
   VlessRelayStatus.update(runtime,snapshot);assertEquals(41L,VlessRelayStatus.snapshot()!!.getLong("generation"))
   snapshot.put("connection_id","bond-42");VlessRelayStatus.update(runtime,snapshot,true)
   VlessRelayStatus.update(runtime,snapshot)
   assertEquals(42L,VlessRelayStatus.snapshot()!!.getLong("generation"));assertEquals(1,VlessRelayStatus.snapshot()!!.getInt("lostSessions"))
  }finally{VlessRelayStatus.clear()}
 }

}
