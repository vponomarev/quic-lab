package ru.vpnc.quiclab

import android.content.Context
import android.content.ContextWrapper
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.io.File

/** Opt-in process-death test; persistent fixture is separate from user configuration. */
class ConfigurationTransferCrashTest {
 private fun context():Context {
  val real=InstrumentationRegistry.getInstrumentation().targetContext
  return object:ContextWrapper(real){
   override fun getNoBackupFilesDir()=File(real.noBackupFilesDir,"transfer-process-crash").apply{mkdirs()}
   override fun getFilesDir()=noBackupFilesDir
   override fun getApplicationContext():Context=this
   override fun getSharedPreferences(name:String,mode:Int)=real.getSharedPreferences("transfer-process-crash-$name",mode)
  }
 }
 @Test fun interruptApply(){
  val stage=InstrumentationRegistry.getArguments().getString("transferCrash")
  assumeTrue(stage=="before" || stage=="after")
  val c=context();val store=MdmConfigurationStore(c)
  // Fresh isolated baseline; no real profile or VPN port is involved.
  VpnProfiles.list(c)
  val base=MdmConfiguration.snapshotLocal(c)
  base.getJSONObject("diagnostics").put("enabled",true).put("detailed",false)
  store.initialize(base,MdmConfiguration.compile(base,MdmConfiguration.snapshotRuntime(c)))
  val desired=JSONObject(base.toString()).apply{getJSONObject("diagnostics").put("detailed",true)}
  val bundle=mobile.Mobile.exportConfiguration(desired.toString(),"""{"sections":["diagnostics"]}""",false)
  val preview=ConfigurationTransfer.prepare(c,bundle,"update",emptyMap())
  var active=true
  val port=object:MdmVpnPort{
   override fun active()=active
   override fun stop(){active=false;if(stage=="before")android.os.Process.killProcess(android.os.Process.myPid())}
   override fun start():String{android.os.Process.killProcess(android.os.Process.myPid());return "unreachable"}
  }
  LocalConfigurationApply.apply(c,preview,port)
  fail("process was expected to terminate")
 }
 @Test fun verifyRecovery(){
  val stage=InstrumentationRegistry.getArguments().getString("transferCrash")
  assumeTrue(stage=="before" || stage=="after")
  val c=context();val store=MdmConfigurationStore(c)
  assertNotNull("operation must survive process death",store.localOperation())
  assertEquals(stage=="after",store.effective()!!.getJSONObject("diagnostics").getBoolean("detailed"))
  var starts=0
  val port=object:MdmVpnPort{
   override fun active()=false
   override fun stop(){fail("recovery must not stop")}
   override fun start():String{starts++;return "starting"}
  }
  LocalConfigurationApply.recover(c,port)
  assertEquals(1,starts);assertNull(MdmConfigurationStore(c).localOperation())
  assertEquals(stage=="after",MdmConfigurationStore(c).localRollback()!=null)
  LocalConfigurationApply.recover(c,port);assertEquals(1,starts)
  c.noBackupFilesDir.deleteRecursively()
  for(name in listOf("vpn","vpn_profiles","vpn_budget","vpn_rtt","vpn_reserve","diagnostics_settings"))
   InstrumentationRegistry.getInstrumentation().targetContext.deleteSharedPreferences("transfer-process-crash-$name")
 }
}