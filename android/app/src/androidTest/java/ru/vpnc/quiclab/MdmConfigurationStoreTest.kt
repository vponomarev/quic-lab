package ru.vpnc.quiclab

import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File

class MdmConfigurationStoreTest {
 private fun document(name:String)=JSONObject("""{"schema":1,"profiles":[{"id":"default","name":"$name","settings":{"transport":"quic","endpoint":"vpn.example:443","mode":0}}],"currentProfileId":"default","enabledProfileIds":["default"],"multiple":false,"globalApps":[],"dns":{"mode":"tunnel","profileId":"default"},"budget":{"limitBytes":0},"diagnostics":{"enabled":true,"detailed":false}}""")
 private fun fixture(block:(MdmConfigurationStore,File,String)->Unit){
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val f=File(c.noBackupFilesDir,"mdm-config-test-"+System.nanoTime())
  val alias="mdm-config-test-"+System.nanoTime()
  try{block(MdmConfigurationStore(f,alias),f,alias)}
  finally{f.delete();File(f.path+".bak").delete();File(f.path+".new").delete();java.security.KeyStore.getInstance("AndroidKeyStore").apply{load(null);deleteEntry(alias)}}
 }
 @Test fun currentPauseKeepsEdits()=fixture{s,_,_->
  s.initialize(document("Personal"))
  s.apply("binding",MdmConfigRevision(1,"current",document("Changed")))
  s.detach()
  assertEquals("Changed",s.effective()!!.getJSONArray("profiles").getJSONObject(0).getString("name"))
  assertEquals(0L,s.revision("binding"))
 }
 @Test fun externalPauseRestoresPersonalAndDoesNotRetainExternal()=fixture{s,f,a->
  val personal=document("Personal")
  s.initialize(personal)
  s.apply("binding",MdmConfigRevision(1,"external",document("External-secret-marker")))
  assertEquals("External-secret-marker",s.effective()!!.getJSONArray("profiles").getJSONObject(0).getString("name"))
  val restarted=MdmConfigurationStore(f,a)
  assertEquals(s.effective().toString(),restarted.effective().toString())
  restarted.detach()
  assertEquals(personal.toString(),restarted.effective().toString())
  assertFalse(MdmStore(f,a).document().toString().contains("External-secret-marker"))
  assertFalse(File(f.path+".bak").exists())
 }
 @Test fun modeTransitionAndDuplicateRevision()=fixture{s,_,_->
  s.initialize(document("Personal"))
  s.apply("binding",MdmConfigRevision(1,"current",document("Current")))
  s.apply("binding",MdmConfigRevision(2,"external",document("External")))
  assertFalse(s.apply("binding",MdmConfigRevision(2,"external",document("Must-not-apply"))))
  s.detach()
  assertEquals("Current",s.effective()!!.getJSONArray("profiles").getJSONObject(0).getString("name"))
  s.apply("new-binding",MdmConfigRevision(1,"external",document("New-external")))
  s.apply("new-binding",MdmConfigRevision(2,"current",document("Promoted")))
  s.detach()
  assertEquals("Promoted",s.effective()!!.getJSONArray("profiles").getJSONObject(0).getString("name"))
 }
 @Test fun invalidAndFailedWritesDoNotPublishRevision()=fixture{s,f,a->
  s.initialize(document("Personal"))
  assertTrue(runCatching{s.apply("binding",MdmConfigRevision(1,"current",JSONObject("""{"schema":2}""")))}.isFailure)
  assertEquals(0L,s.revision("binding"))
  val fail=MdmConfigurationStore(f,a,{error("disk unavailable")})
  assertTrue(runCatching{fail.apply("binding",MdmConfigRevision(1,"current",document("Lost")))}.isFailure)
  assertEquals("Personal",s.effective()!!.getJSONArray("profiles").getJSONObject(0).getString("name"))
  assertEquals(0L,s.revision("binding"))
 }

 @Test fun credentialsArePreservedOnlyForSameProfileAndCanBeRemoved()=fixture{s,f,_->
  val personal=document("Personal")
  val secret="vless://11111111-1111-4111-8111-111111111111@outer.invalid:443?encryption=none&security=tls&type=tcp&sni=server.invalid&fp=chrome"
  personal.getJSONArray("profiles").getJSONObject(0).put("identity",JSONObject().put("vless_uri",secret))
  s.initialize(personal)
  s.apply("binding",MdmConfigRevision(1,"external",document("External")))
  assertEquals(secret,s.effective()!!.getJSONArray("profiles").getJSONObject(0).getJSONObject("identity").getString("vless_uri"))
  assertFalse(f.readText().contains("11111111"))
  assertTrue(runCatching{s.apply("other",MdmConfigRevision(2,"current",document("Other")))}.isFailure)
  val removed=document("Without keys");removed.getJSONArray("profiles").getJSONObject(0).put("removeIdentity",true)
  s.apply("binding",MdmConfigRevision(2,"external",removed))
  assertFalse(s.effective()!!.getJSONArray("profiles").getJSONObject(0).has("identity"))
  s.detach()
  assertEquals(personal.toString(),s.effective().toString())
 }
}
