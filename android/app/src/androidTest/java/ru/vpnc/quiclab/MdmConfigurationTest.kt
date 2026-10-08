package ru.vpnc.quiclab

import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.UUID
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class MdmConfigurationTest {
 private class Isolated(base:Context):ContextWrapper(base){
  val prefix="mdm-adapter-"+UUID.randomUUID();val names=mutableSetOf<String>()
  val dir=File(base.cacheDir,prefix).apply{mkdirs()}
  override fun getFilesDir()=dir
  override fun getNoBackupFilesDir()=File(dir,"private").apply{mkdirs()}
  override fun getSharedPreferences(name:String,mode:Int):SharedPreferences {
   names.add(prefix+name);return baseContext.getSharedPreferences(prefix+name,mode)
  }
  fun close(){names.forEach{baseContext.deleteSharedPreferences(it)};dir.deleteRecursively()}
 }
 private fun fixture(block:(Isolated)->Unit){
  val c=Isolated(InstrumentationRegistry.getInstrumentation().targetContext)
  try{
   VpnProfiles.list(c)
   VpnProfiles.preferences(c).edit().putString("endpoint","personal.example:443").putString("transport","quic").putInt("mode",0).commit()
   VpnIdentity.writeBundle(c,"default",JSONObject().put("subject","Personal").put("device_id","original-device").put("update_token","a".repeat(64)))
   MdmStore(c).edit{it.put("binding",MdmStore.bindingJson(MdmBinding("binding","https://mdm.example",1,MdmRights(config=true),true)))
    .put("active",true).put("rights",MdmRights(config=true).json()).put("generation",7)}
   block(c)
  }finally{c.close()}
 }
 private fun desired(mode:String,revision:Long=1)=MdmConfigRevision(revision,mode,JSONObject("""{"schema":1,"profiles":[{"id":"default","name":"Managed","settings":{"transport":"quic","endpoint":"managed.example:443","mode":0}}],"currentProfileId":"default","enabledProfileIds":["default"],"multiple":false,"globalApps":["example.managed"],"dns":{"mode":"system","profileId":"default"},"budget":{"limitBytes":1048576},"diagnostics":{"enabled":false,"detailed":false}}"""))
 @Test fun authoritativeReadersAndExternalRestore()=fixture{c->
  MdmConfiguration.apply(c,desired("external"),7)
  assertEquals("Managed",VpnProfiles.current(c).name)
  assertEquals("managed.example:443",VpnProfiles.preferences(c).getString("endpoint",""))
  assertEquals(setOf("example.managed"),VpnProfiles.globalApps(c))
  assertEquals("system",VpnProfiles.dnsMode(c))
  assertEquals(1048576L,VpnBudgetSettings.limitBytes(c))
  assertFalse(DiagnosticsPolicy.enabled(c))
  assertEquals("original-device",VpnIdentity.load(c).getString("device_id"))
  assertTrue(VpnConfiguration.load(c).toString().contains("managed.example"))
  assertFalse(File(c.filesDir,"vpn-configuration.json").let{it.exists()&&it.readText().contains("managed.example")})
  // Even later legacy writes cannot override the authoritative layer.
  c.getSharedPreferences("vpn",0).edit().putString("endpoint","stale.example:443").commit()
  assertEquals("managed.example:443",VpnProfiles.preferences(c).getString("endpoint",""))
  MdmStore(c).edit{it.put("active",false).put("generation",8)}
  MdmConfiguration.detach(c)
  assertEquals("personal.example:443",VpnProfiles.preferences(c).getString("endpoint",""))
  assertEquals("original-device",VpnIdentity.load(c).getString("device_id"))
 }
 @Test fun currentRetainedAndLocalEditingResumes()=fixture{c->
  MdmConfiguration.apply(c,desired("current"),7)
  assertTrue(runCatching{VpnProfiles.rename(c,"Forbidden")}.isFailure)
  assertTrue(runCatching{VpnProfiles.setGlobalApps(c,setOf("example.forbidden"))}.isFailure)
  assertTrue(runCatching{VpnIdentity.writeBundle(c,"default",JSONObject().put("subject","Forbidden"))}.isFailure)
  MdmStore(c).edit{it.put("active",false).put("generation",8)}
  MdmConfiguration.detach(c)
  VpnProfiles.rename(c,"Local")
  VpnProfiles.preferences(c).edit().putString("endpoint","edited.example:443").commit()
  assertEquals("Local",VpnProfiles.current(c).name)
  assertEquals("edited.example:443",VpnProfiles.preferences(c).getString("endpoint",""))
  assertEquals("original-device",VpnIdentity.load(c).getString("device_id"))
  VpnBudgetSettings.preferences(c).edit().putLong("cell_mib",2).commit()
  assertEquals(2097152L,VpnBudgetSettings.limitBytes(c))
 }
 @Test fun oldDraftCannotResurrectExternalAndStaleGenerationRejected()=fixture{c->
  assertTrue(runCatching{MdmConfiguration.apply(c,desired("external"),6)}.isFailure)
  MdmConfiguration.apply(c,desired("external"),7)
  val draft=PreferenceDraft(VpnProfiles.preferences(c))
  draft.edit().putString("endpoint","resurrect.example:443").commit()
  MdmStore(c).edit{it.put("active",false).put("generation",8)}
  MdmConfiguration.detach(c)
  assertTrue(runCatching{draft.persist()}.isFailure)
  assertEquals("personal.example:443",VpnProfiles.preferences(c).getString("endpoint",""))
 }

 @Test fun explicitKeyRemovalDoesNotFallBackToLegacyFile()=fixture{c->
  val d=desired("external")
  d.document.getJSONArray("profiles").getJSONObject(0).put("removeIdentity",true)
  MdmConfiguration.apply(c,d,7)
  assertFalse(VpnIdentity.exists(c,"default"))
  assertTrue(runCatching{VpnIdentity.load(c)}.isFailure)
  MdmStore(c).edit{it.put("active",false).put("generation",8)}
  MdmConfiguration.detach(c)
  assertEquals("original-device",VpnIdentity.load(c).getString("device_id"))
 }

 @Test fun profileUpdatesRemainEffectiveAfterCurrentManagement()=fixture{c->
  MdmConfiguration.apply(c,desired("current"),7)
  MdmStore(c).edit{it.put("active",false).put("generation",8)}
  MdmConfiguration.detach(c)
  val bundle=VpnIdentity.load(c)
  bundle.put("server_config",JSONObject().put("version",2).put("transports",org.json.JSONArray().put("quic"))
   .put("quic","updated.example:443").put("hostname","updated.example"))
  VpnIdentity.writeBundle(c,"default",bundle)
  assertEquals("updated.example:443",VpnProfiles.preferences(c).getString("endpoint",""))
 }
 @Test fun preferenceListenersSurviveAuthorityChange()=fixture{c->
  MdmConfiguration.apply(c,desired("current"),7)
  MdmStore(c).edit{it.put("active",false).put("generation",8)}
  MdmConfiguration.detach(c)
  val delivered=java.util.concurrent.CountDownLatch(1)
  val listener=SharedPreferences.OnSharedPreferenceChangeListener{_,key->if(key=="probe")delivered.countDown()}
  val p=VpnRttSettings.preferences(c)
  p.registerOnSharedPreferenceChangeListener(listener)
  try{
   VpnRttSettings.preferences(c).edit().putInt("probe",123).commit()
   assertTrue(delivered.await(5,java.util.concurrent.TimeUnit.SECONDS))
  }finally{p.unregisterOnSharedPreferenceChangeListener(listener)}
 }

 @Test fun recreatedEditorCannotRestoreExternalDraftIntoPersonal()=fixture{c->
  MdmConfiguration.apply(c,desired("external"),7)
  val draft=PreferenceDraft(VpnProfiles.preferences(c))
  draft.edit().putString("endpoint","old-external.example:443").commit()
  val saved=draft.snapshot()
  MdmStore(c).edit{it.put("active",false).put("generation",8)}
  MdmConfiguration.detach(c)
  val recreated=PreferenceDraft(VpnProfiles.preferences(c))
  recreated.restore(saved)
  assertTrue(runCatching{recreated.persist()}.isFailure)
  val recreatedAgain=PreferenceDraft(VpnProfiles.preferences(c))
  recreatedAgain.restore(recreated.snapshot())
  assertTrue(runCatching{recreatedAgain.persist()}.isFailure)
  assertEquals("personal.example:443",VpnProfiles.preferences(c).getString("endpoint",""))
 }
}
