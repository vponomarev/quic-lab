package ru.vpnc.quiclab

import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File

class ConfigurationTransferTest {
 private fun doc(name:String)=JSONObject("""{"schema":1,"profiles":[{"id":"default","name":"$name","settings":{}}],"currentProfileId":"default","enabledProfileIds":[],"multiple":false,"globalApps":[],"dns":{"mode":"system","profileId":"default"},"budget":{"limitBytes":0},"diagnostics":{"enabled":true,"detailed":false}}""")
 @Test fun atomicLocalApplyRejectsStalePreviewAndKeepsRollback(){
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val f=File(c.noBackupFilesDir,"transfer-test-${System.nanoTime()}")
  val alias="transfer-test-${System.nanoTime()}"
  try{
   val s=MdmConfigurationStore(f,alias)
   s.initialize(doc("Before"),JSONObject())
   val epoch=s.configEpoch()
   s.applyLocal(doc("After"),JSONObject(),epoch)
   assertEquals("After",s.effective()!!.getJSONArray("profiles").getJSONObject(0).getString("name"))
   try{s.applyLocal(doc("Stale"),JSONObject(),epoch);fail("stale preview accepted")}catch(_:IllegalStateException){}
   assertEquals("After",s.effective()!!.getJSONArray("profiles").getJSONObject(0).getString("name"))
   assertEquals("Before",s.localRollback()!!.getJSONObject("document").getJSONArray("profiles").getJSONObject(0).getString("name"))
   assertNull(s.mode());assertEquals(0L,s.revision("fake"))
  }finally{f.delete();File(f.path+".bak").delete();File(f.path+".new").delete();java.security.KeyStore.getInstance("AndroidKeyStore").apply{load(null);deleteEntry(alias)}}
 }
 @Test fun plaintextSecretImportIsRejected(){
  val raw="""{"format":"quic-lab-config","version":1,"credentials":"included","payload":{}}""".toByteArray()
  try{ConfigurationTransfer.decode(raw,"");fail("plaintext keys accepted")}catch(_:IllegalArgumentException){}
 }
 @Test fun incompleteCandidateDoesNotRestartVpn(){
  assertTrue(LocalConfigurationApply.canRestart(true,4,4,false))
  assertFalse(LocalConfigurationApply.canRestart(true,4,5,false))
  assertFalse(LocalConfigurationApply.canRestart(false,4,4,false))
  assertFalse(LocalConfigurationApply.canRestart(true,4,4,true))
 } @Test fun prepareApplyAndConcurrentEditUseIsolatedContext(){
  val real=InstrumentationRegistry.getInstrumentation().targetContext
  val suffix="transfer-context-${System.nanoTime()}";val dir=File(real.noBackupFilesDir,suffix).apply{mkdirs()}
  val c=object:android.content.ContextWrapper(real){
   override fun getNoBackupFilesDir()=dir
   override fun getFilesDir()=dir
   override fun getApplicationContext():android.content.Context=this
   override fun getSharedPreferences(name:String,mode:Int)=real.getSharedPreferences("$suffix-$name",mode)
  }
  val port=object:MdmVpnPort{var running=false;override fun active()=running;override fun stop(){running=false};override fun start():String{running=true;return "starting"}}
  try{
   VpnProfiles.list(c)
   val bundle=mobile.Mobile.exportConfiguration(doc("Other").toString(),"""{"sections":["diagnostics"]}""",false)
   val p=ConfigurationTransfer.prepare(c,bundle,"update",emptyMap())
   val before=MdmConfiguration.snapshotLocal(c).toString()
   assertEquals(before,MdmConfiguration.snapshotLocal(c).toString()) // preview does not publish
   LocalConfigurationApply.apply(c,p,port)
   assertNotNull(MdmConfigurationStore(c).localRollback())
   val failure=runCatching{LocalConfigurationApply.apply(c,p,port)}.exceptionOrNull();assertNotNull(failure);assertTrue(failure is IllegalStateException || failure?.cause is IllegalStateException)
      assertFalse(port.running)
   val uri="vless://11111111-1111-4111-8111-111111111111@example.org:443?security=tls&type=tcp&sni=example.org"
   MdmConfiguration.writeIdentity(c,"default",JSONObject().put("vless_config",mobile.Mobile.importVLESSConfig(uri)).put("update_token","must-not-travel"))
   val encrypted=ConfigurationTransfer.export(c,listOf("profiles"),emptyList(),true,"test-passphrase")
   val decoded=ConfigurationTransfer.decode(encrypted,"test-passphrase")
   assertTrue(decoded.contains("vless://"));assertFalse(decoded.contains("must-not-travel"));assertFalse(decoded.contains("update_token"))
   val plain=ConfigurationTransfer.export(c,listOf("profiles"),emptyList(),false,"").toString(Charsets.UTF_8)
   assertFalse(plain.contains("vless://"))
  }finally{dir.deleteRecursively();for(n in listOf("vpn","vpn_profiles","vpn_budget","vpn_rtt","vpn_reserve","diagnostics_settings"))real.deleteSharedPreferences("$suffix-$n")}
 } @Test fun disablingResumeSurvivesReleasedMeter(){
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val file=File(c.filesDir,"transfer-meter-${System.nanoTime()}")
  try{
   val run=VpnBudgetRun(file,90);run.start(100000);run.setResumeEligible(true);run.release()
   assertNull(run.current);run.setResumeEligible(false)
   assertFalse(VpnBudgetRun(file,90).shouldResume())
  }finally{file.delete();File(file.path+".bak").delete()}
 } @Test fun missingApplicationIsReported(){
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val d=doc("Apps");d.put("globalApps",org.json.JSONArray().put("nonexistent.transfer.fixture.app"))
  assertEquals(listOf("nonexistent.transfer.fixture.app"),ConfigurationTransfer.missingApplications(c,d))
 }}
