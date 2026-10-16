package ru.vpnc.quiclab

import android.util.Base64
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class VlessSubscriptionsTest {
 private val uri="vless://11111111-1111-4111-8111-111111111111@vpn.example.org:443?security=tls&type=tcp&sni=vpn.example.org#One"
 @Test fun plainBase64AndRotation(){
  val a=VlessSubscriptions.parse(uri.toByteArray());assertEquals(1,a.entries.size)
  val b=VlessSubscriptions.parse(Base64.encode(uri.toByteArray(),Base64.NO_WRAP));assertEquals(a.entries[0].key,b.entries[0].key)
  val rotated=uri.replace("11111111-1111-4111-8111-111111111111","22222222-2222-4222-8222-222222222222").replace("#One","#Renamed")
  assertEquals(a.entries[0].key,VlessSubscriptions.parse(rotated.toByteArray()).entries[0].key)
 }
 @Test fun invalidAndUnsupported(){
  for(raw in listOf("", "<html>no</html>",uri.replace("type=tcp","type=ws"))){try{VlessSubscriptions.parse(raw.toByteArray());fail("accepted invalid/all unsupported")}catch(_:IllegalArgumentException){}}
  val parsed=VlessSubscriptions.parse((uri+"\n"+uri.replace("type=tcp","type=ws")).toByteArray());assertEquals(1,parsed.skipped)
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
 @Test fun mixedProtocols(){val p=VlessSubscriptions.parse(("trojan://unsupported@example.org:443\n"+uri).toByteArray());assertEquals(1,p.entries.size);assertEquals(1,p.skipped)}
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
