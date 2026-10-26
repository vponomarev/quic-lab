package ru.vpnc.quiclab

import android.util.Base64
import mobile.Mobile
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class VlessSubscriptionsTest {
 @Test fun full256NodeSubscriptionImportsAsOneGroup(){
  val raw=(0 until 256).joinToString("\n"){uri.replace("vpn.example.org","node$it.example.org")}
  val parsed=VlessSubscriptions.parse(raw.toByteArray())
  val doc=JSONObject("""{"schema":1,"profiles":[],"currentProfileId":"default","enabledProfileIds":[],"multiple":false,"globalApps":[],"dns":{"mode":"tunnel","profileId":"default"},"budget":{"limitBytes":0},"diagnostics":{"enabled":true,"detailed":false}}""")
  val merged=VlessSubscriptions.mergeGroup(doc,"group","Full subscription",parsed)
  val profile=merged.getJSONArray("profiles").getJSONObject(0)
  assertEquals(1,merged.getJSONArray("profiles").length())
  assertEquals(256,profile.getJSONObject("identity").getJSONArray("vless_uris").length())
  merged.put("currentProfileId",profile.getString("id"))
  merged.getJSONObject("dns").put("profileId",profile.getString("id"))
  Mobile.validateMDMConfiguration(merged.toString())
 }
 @Test fun groupLimitIncludesTemporarilyMissingNodesWithoutMutatingPrior(){
  val before=VlessSubscriptions.mergeGroup(JSONObject().put("profiles",org.json.JSONArray()),"group","Before",VlessSubscriptions.parse(uri.toByteArray()))
  val saved=before.toString()
  val raw=(0 until 256).joinToString("\n"){uri.replace("vpn.example.org","new$it.example.org")}
  try {VlessSubscriptions.mergeGroup(before,"group","After",VlessSubscriptions.parse(raw.toByteArray()));fail("257 retained nodes accepted")}
  catch(_:IllegalArgumentException){}
  assertEquals(saved,before.toString())
 }
 private val uri="vless://11111111-1111-4111-8111-111111111111@vpn.example.org:443?security=tls&type=tcp&sni=vpn.example.org#One"
 private fun legacyKey()=java.security.MessageDigest.getInstance("SHA-256").digest("vpn.example.org:443|tls|".toByteArray()).joinToString(""){"%02x".format(it)}
 private fun legacyId(group:String)=java.util.UUID.nameUUIDFromBytes(("vless-subscription:"+group+":"+legacyKey()).toByteArray()).toString()
 private fun legacySnapshot(group:String):JSONObject {
  val id=legacyId(group)
  return JSONObject().put("currentProfileId",id).put("profiles",org.json.JSONArray().put(JSONObject()
   .put("id",id).put("name","My route").put("identity",JSONObject().put("vless_uri",uri))
   .put("settings",JSONObject().put("mode",1).put("routes","192.0.2.0/24").put("relay_exit_profile_id","gateway"))))
 }
 @Test fun legacyRefreshKeepsSelectedProfileAndLocalRules(){
  val before=legacySnapshot("g");val original=before.toString()
  val rotated=uri.replace("11111111-1111-4111-8111-111111111111","22222222-2222-4222-8222-222222222222").replace("sni=vpn.example.org","sni=rotated.example.org")
  val after=VlessSubscriptions.merge(before,"g",VlessSubscriptions.parse(rotated.toByteArray()))
  assertEquals(1,after.getJSONArray("profiles").length())
  val profile=after.getJSONArray("profiles").getJSONObject(0)
  assertEquals(legacyId("g"),profile.getString("id"));assertEquals(legacyId("g"),after.getString("currentProfileId"))
  assertEquals("My route",profile.getString("name"));assertEquals("192.0.2.0/24",profile.getJSONObject("settings").getString("routes"))
  assertEquals("gateway",profile.getJSONObject("settings").getString("relay_exit_profile_id"))
  assertEquals(rotated,profile.getJSONObject("identity").getString("vless_uri"));assertEquals(original,before.toString())
 }
 @Test fun legacyTCPIsNotReplacedByWebSocketAtSameEndpoint(){
  val after=VlessSubscriptions.merge(legacySnapshot("g"),"g",VlessSubscriptions.parse(uri.replace("type=tcp","type=ws&path=%2Fone").toByteArray()))
  assertEquals(2,after.getJSONArray("profiles").length())
  assertEquals(uri,after.getJSONArray("profiles").getJSONObject(0).getJSONObject("identity").getString("vless_uri"))
 }
 @Test fun legacySelectionAndProbeMembershipKeepOriginalProfile(){
  val before=legacySnapshot("g");val key=VlessSubscriptions.parse(uri.toByteArray()).entries.single().key
  assertEquals(setOf(key,"temporarily-missing"),VlessSubscriptions.normalizeSelected(before,"g",setOf(legacyKey(),"temporarily-missing")))
  assertEquals(setOf(legacyId("g")),VlessSubscriptions.subscriptionProfileIds(before,"g",setOf(key)))
  assertTrue(VlessSubscriptions.subscriptionProfileIds(before,"other",setOf(key)).isEmpty())
 }
 @Test fun legacyGroupedSelectionMigratesWithoutChangingGroupId(){
  val before=VlessSubscriptions.mergeGroup(JSONObject().put("profiles",org.json.JSONArray()),"g","Group",VlessSubscriptions.parse(uri.toByteArray()))
  val key=VlessSubscriptions.parse(uri.toByteArray()).entries.single().key
  assertEquals(setOf(key),VlessSubscriptions.normalizeSelected(before,"g",setOf(legacyKey())))
 }
 @Test fun membershipSnapshotKeepsWebSocketAndIgnoresUnrelatedSecrets(){
  val ws=uri.replace("type=tcp","type=ws&path=%2Ftunnel&host=front.example")
  val imported=JSONObject(Mobile.importVLESSConfig(ws));imported.getJSONObject("config").put("RootPEM","private-ca")
  val snapshot=VlessSubscriptions.membershipSnapshot(mapOf("ws" to JSONObject().put("vless_config",imported.toString()).put("key","private-key"),"other" to JSONObject().put("key","unrelated-key")))
  assertEquals(1,snapshot.getJSONArray("profiles").length())
  val raw=snapshot.getJSONArray("profiles").getJSONObject(0).getJSONObject("identity").getString("vless_uri")
  val config=JSONObject(Mobile.importVLESSConfig(raw)).getJSONObject("config")
  assertEquals("ws",config.getString("Transport"));assertEquals("/tunnel",config.getString("WebSocketPath"));assertEquals("front.example",config.getString("WebSocketHost"))
  assertFalse(snapshot.toString().contains("private-ca"));assertFalse(snapshot.toString().contains("private-key"));assertFalse(snapshot.toString().contains("unrelated-key"))
 }
 @Test fun plainBase64AndRotation(){
  val a=VlessSubscriptions.parse(uri.toByteArray());assertEquals(1,a.entries.size)
  val b=VlessSubscriptions.parse(Base64.encode(uri.toByteArray(),Base64.NO_WRAP));assertEquals(a.entries[0].key,b.entries[0].key)
  val rotated=uri.replace("11111111-1111-4111-8111-111111111111","22222222-2222-4222-8222-222222222222").replace("#One","#Renamed")
  assertEquals(a.entries[0].key,VlessSubscriptions.parse(rotated.replace("sni=vpn.example.org","sni=rotated.example.org").toByteArray()).entries[0].key)
 }
 @Test fun invalidAndUnsupported(){
  for(raw in listOf("", "<html>no</html>",uri.replace("type=tcp","type=grpc"))){try{VlessSubscriptions.parse(raw.toByteArray());fail("accepted invalid/all unsupported")}catch(_:IllegalArgumentException){}}
  val parsed=VlessSubscriptions.parse((uri+"\n"+uri.replace("type=tcp","type=grpc")).toByteArray());assertEquals(1,parsed.skipped)
  try{VlessSubscriptions.parse((uri+"\n"+uri.replace("#One","#Other")).toByteArray());fail("ambiguous identity")}catch(_:IllegalArgumentException){}
 }
 @Test fun refreshPreservesLocalSettings(){
  val doc=JSONObject("""{"profiles":[],"currentProfileId":"default"}""")
  val a=VlessSubscriptions.merge(doc,"group",VlessSubscriptions.parse(uri.toByteArray()))
  val profile=a.getJSONArray("profiles").getJSONObject(0);profile.getJSONObject("settings").put("mode",1).put("apps",org.json.JSONArray().put("org.telegram.messenger"))
  val b=VlessSubscriptions.merge(a,"group",VlessSubscriptions.parse(uri.replace("#One","#New").toByteArray()))
  assertEquals(1,b.getJSONArray("profiles").length());assertEquals(profile.getString("id"),b.getJSONArray("profiles").getJSONObject(0).getString("id"))
  assertEquals(1,b.getJSONArray("profiles").getJSONObject(0).getJSONObject("settings").getInt("mode"));assertEquals("default",b.getString("currentProfileId"))
 }
 @Test fun groupRefreshPreservesIdentityAndRouting(){
  val doc=JSONObject("""{"profiles":[],"currentProfileId":"default"}""")
  val parsed=VlessSubscriptions.parse((uri+"\n"+uri.replace("vpn.example.org","second.example.org")).toByteArray())
  val a=VlessSubscriptions.mergeGroup(doc,"group","Fixture",parsed)
  val profile=a.getJSONArray("profiles").getJSONObject(0)
  profile.getJSONObject("settings").put("mode",1).put("apps",org.json.JSONArray().put("org.telegram.messenger"))
  assertEquals(2,profile.getJSONObject("identity").getJSONArray("vless_uris").length())
  val b=VlessSubscriptions.mergeGroup(a,"group","Fixture",parsed.copy(entries=parsed.entries.reversed()))
  val updated=b.getJSONArray("profiles").getJSONObject(0)
  assertEquals(1,b.getJSONArray("profiles").length());assertEquals(profile.getString("id"),updated.getString("id"))
  assertEquals(1,updated.getJSONObject("settings").getInt("mode"));assertEquals("default",b.getString("currentProfileId"))
  assertEquals(parsed.entries.last().uri,updated.getJSONObject("identity").getString("vless_uri"))
 }
 @Test fun groupKeepsTemporarilyMissingSelectedMember(){
  val full=VlessSubscriptions.parse((uri+"\n"+uri.replace("vpn.example.org","second.example.org")).toByteArray())
  val a=VlessSubscriptions.mergeGroup(JSONObject("""{"profiles":[],"currentProfileId":"default"}"""),"g","Group",full)
  val missing=VlessSubscriptions.parse(uri.toByteArray())
  val b=VlessSubscriptions.mergeGroup(a,"g","Group",missing)
  assertEquals(2,b.getJSONArray("profiles").getJSONObject(0).getJSONObject("identity").getJSONArray("vless_uris").length())
  val c=VlessSubscriptions.mergeGroup(b,"g","Group",full)
  assertEquals(2,c.getJSONArray("profiles").getJSONObject(0).getJSONObject("identity").getJSONArray("vless_uris").length())
  val deselected=VlessSubscriptions.mergeGroup(c,"g","Group",full.copy(entries=full.entries.take(1)))
  assertEquals(1,deselected.getJSONArray("profiles").getJSONObject(0).getJSONObject("identity").getJSONArray("vless_uris").length())
 }
 @Test fun mixedProtocols(){val p=VlessSubscriptions.parse(("trojan://unsupported@example.org:443\n"+uri).toByteArray());assertEquals(1,p.entries.size);assertEquals(1,p.skipped)}
 @Test fun xrayArrayAndEmptyTCPSettings(){
  val outbound=JSONObject("""{"protocol":"vless","tag":"proxy","settings":{"vnext":[{"address":"vpn.example.org","port":443,"users":[{"id":"11111111-1111-4111-8111-111111111111","encryption":"none"}]}]},"streamSettings":{"network":"tcp","security":"tls","tlsSettings":{"serverName":"vpn.example.org"},"tcpSettings":{}}}""")
  val doc=JSONObject().put("remarks","Fixture").put("outbounds",org.json.JSONArray().put(outbound).put(JSONObject().put("protocol","freedom")))
  val p=VlessSubscriptions.parse(org.json.JSONArray().put(doc).toString().toByteArray())
  assertEquals(1,p.entries.size);assertEquals("Fixture",p.entries[0].name);assertTrue(p.entries[0].uri.startsWith("vless://"));VlessImport.metadata(p.entries[0].uri)
  outbound.put("proxySettings",JSONObject().put("tag","chain"))
  try{VlessSubscriptions.parse(org.json.JSONArray().put(doc).toString().toByteArray());fail("chaining accepted")}catch(_:IllegalArgumentException){}
 }

 @Test fun xraySubscriptionKeepsEveryVlessOutboundIncludingWebSocket(){
  val reality=JSONObject("""{"tag":"reality-v6","protocol":"vless","settings":{"vnext":[{"address":"2001:db8::4","port":8443,"users":[{"id":"11111111-1111-4111-8111-111111111111","encryption":"none","flow":"xtls-rprx-vision"}]}]},"streamSettings":{"network":"tcp","security":"reality","realitySettings":{"serverName":"cover.example","fingerprint":"firefox","publicKey":"AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE","shortId":"aabb"}}}""")
  val websocket=JSONObject("""{"tag":"websocket-fallback","protocol":"vless","settings":{"vnext":[{"address":"fallback.example","port":443,"users":[{"id":"11111111-1111-4111-8111-111111111111","encryption":"none"}]}]},"streamSettings":{"network":"ws","security":"tls","wsSettings":{"path":"/tunnel","headers":{"Host":"fallback.example"}},"tlsSettings":{"serverName":"fallback.example","fingerprint":"edge"}},"mux":{"enabled":true,"concurrency":8}}""")
  val document=JSONObject().put("remarks","Swiss group").put("outbounds",org.json.JSONArray().put(reality).put(websocket).put(JSONObject().put("protocol","freedom")))
  val parsed=VlessSubscriptions.parse(org.json.JSONArray().put(document).toString().toByteArray())
  assertEquals(2,parsed.entries.size);assertEquals(0,parsed.skipped)
  val configs=parsed.entries.map{JSONObject(Mobile.importVLESSConfig(it.uri)).getJSONObject("config")}
  assertTrue(configs.any{it.optString("Transport")=="ws" && it.optString("WebSocketPath")=="/tunnel"})
  assertTrue(configs.any{it.optString("Endpoint")=="[2001:db8::4]:8443" && it.optString("Security")=="reality"})
 }
 @Test fun rejectDuplicateJSONKeys(){
  try{VlessSubscriptions.parse("""[{"protocol":"vless","protocol":"vless","settings":{"vnext":[{"address":"vpn.example.org","port":443,"users":[{"id":"11111111-1111-4111-8111-111111111111","encryption":"none"}]}]},"streamSettings":{"network":"tcp","security":"tls","tlsSettings":{"serverName":"vpn.example.org"}}}]""".toByteArray());fail("duplicate accepted")}catch(_:IllegalArgumentException){}
 }
 @Test fun absentSelectedEntriesRemainSelected(){
  val a=VlessSubscriptions.Entry("a","A","example.org:443",uri)
  val p=VlessSubscriptions.Parsed(listOf(a),0)
  assertEquals(setOf("a","b"),VlessSubscriptions.retainedSelection(setOf("a","b"),p))
  assertEquals(setOf("a"),VlessSubscriptions.retainedSelection(setOf("a","b"),p.copy(availableKeys=setOf("a","b"))))
 }
 @Test fun urlValidation(){for(url in listOf("http://example.org/s","https://user:pass@example.org/s","https://example.org/s#secret")){try{VlessSubscriptions.url(url);fail("unsafe URL")}catch(_:IllegalArgumentException){}};assertEquals("https",VlessSubscriptions.url("https://example.org/s?token=x").protocol)}
 @Test fun encryptedMetadataAndAtomicFailure(){
  val c=androidx.test.platform.app.InstrumentationRegistry.getInstrumentation().targetContext
  val file=java.io.File(c.noBackupFilesDir,"subscription-test-"+System.nanoTime());val alias="subscription-test-"+System.nanoTime()
  try{
   val store=MdmConfigurationStore(file,alias)
   val doc=JSONObject("""{"schema":1,"profiles":[{"id":"default","name":"Personal","settings":{"transport":"quic","endpoint":"vpn.example:443","mode":0}}],"currentProfileId":"default","enabledProfileIds":[],"multiple":false,"globalApps":[],"dns":{"mode":"tunnel","profileId":"default"},"budget":{"limitBytes":0},"diagnostics":{"enabled":true,"detailed":false}}""")
   val runtime=JSONObject().put("preferences",JSONObject()).put("identities",JSONObject())
   store.initialize(doc,runtime)
   val next=VlessSubscriptions.merge(doc,"group",VlessSubscriptions.parse(uri.toByteArray()))
   val compiled=MdmConfiguration.compile(next,runtime).put("subscriptions",JSONObject().put("group",JSONObject().put("url","https://example.org/private-token")))
   store.applyLocal(next,compiled,0,doc)
   assertFalse(file.readText().contains("private-token"));assertFalse(file.readText().contains("11111111"))
   val reopened=MdmConfigurationStore(file,alias)
   assertEquals(2,reopened.effective()!!.getJSONArray("profiles").length())
   assertTrue(MdmConfiguration.compile(next,reopened.runtime()!!).getJSONObject("subscriptions").toString().contains("private-token"))
   val failing=MdmConfigurationStore(file,alias,{error("disk full")})
   assertTrue(runCatching{failing.applyLocal(doc,runtime,1,next)}.isFailure)
   assertEquals(2,reopened.effective()!!.getJSONArray("profiles").length());assertEquals(1L,reopened.configEpoch())
  }finally{file.delete();java.io.File(file.path+".bak").delete();java.io.File(file.path+".new").delete();java.security.KeyStore.getInstance("AndroidKeyStore").apply{load(null);deleteEntry(alias)}}
 }
}
