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
class MdmApplyCoordinatorTest {
 private class Isolated(base:Context):ContextWrapper(base){
  val prefix="mdm-apply-"+UUID.randomUUID();val names=mutableSetOf<String>();val dir=File(base.cacheDir,prefix).apply{mkdirs()}
  override fun getFilesDir()=dir
  override fun getNoBackupFilesDir()=File(dir,"private").apply{mkdirs()}
  override fun getSharedPreferences(name:String,mode:Int):SharedPreferences{names.add(prefix+name);return baseContext.getSharedPreferences(prefix+name,mode)}
  fun close(){names.forEach{baseContext.deleteSharedPreferences(it)};dir.deleteRecursively()}
 }
 private class Port: MdmVpnPort {var running=false;var starts=0;override fun active()=running;override fun stop(){running=false};override fun start():String{running=true;starts++;return "starting"}}
 @Test fun applyRetryAndExternalCleanup(){
  val c=Isolated(InstrumentationRegistry.getInstrumentation().targetContext);val port=Port()
  try{
   VpnProfiles.list(c);VpnProfiles.preferences(c).edit().putString("endpoint","old.example:443").commit()
   MdmStore(c).edit{it.put("binding",MdmStore.bindingJson(MdmBinding("binding","https://mdm.example",1,MdmRights(config=true),true))).put("active",true).put("rights",MdmRights(config=true).json())}
   val doc=MdmConfiguration.snapshotLocal(c);doc.getJSONArray("profiles").getJSONObject(0).put("name","Managed")
   val desired=MdmConfigRevision(1,"external",doc)
   val coordinator=MdmApplyCoordinator(c,port)
   assertTrue(coordinator.apply(MdmStore(c).read(),desired).configurationApplied)
   assertEquals("Managed",VpnProfiles.current(c).name)
   assertTrue(coordinator.apply(MdmStore(c).read(),desired).configurationApplied)
   assertEquals(1L,MdmStore(c).read().appliedRevision)
   MdmStore(c).edit{it.put("active",false).put("generation",1)}
   coordinator.detach();assertNotEquals("Managed",VpnProfiles.current(c).name)
  }finally{c.close()}
 }
 @Test fun staleAuthorizationDoesNotStopVPN(){
  val c=Isolated(InstrumentationRegistry.getInstrumentation().targetContext);val port=Port();port.running=true
  try{
   VpnProfiles.list(c);val state=MdmStore(c).read()
   val result=MdmApplyCoordinator(c,port).apply(state,MdmConfigRevision(1,"current",MdmConfiguration.snapshotLocal(c)))
   assertFalse(result.configurationApplied);assertTrue(port.running)
  }finally{c.close()}
 }
 @Test fun committedCrashRecoversAndStaleDraftConflicts(){
  val c=Isolated(InstrumentationRegistry.getInstrumentation().targetContext);val port=Port()
  try{
   VpnProfiles.list(c)
   MdmStore(c).edit{it.put("binding",MdmStore.bindingJson(MdmBinding("binding","https://mdm.example",1,MdmRights(config=true),true))).put("active",true).put("rights",MdmRights(config=true).json())}
   val generation=MdmDeviceReport.configurationGeneration(c)
   val doc=MdmConfiguration.snapshotLocal(c);doc.getJSONArray("profiles").getJSONObject(0).put("name","Survived")
   val stale=MdmApplyCoordinator(c,port).apply(MdmStore(c).read(),MdmConfigRevision(1,"current",doc,generation+1))
   assertFalse(stale.configurationApplied);assertEquals("generation_conflict",stale.errorCode)
   val crashing=MdmApplyCoordinator(c,port){phase->if(phase=="committed")throw AssertionError("process death")}
   assertTrue(runCatching{crashing.apply(MdmStore(c).read(),MdmConfigRevision(1,"current",doc,generation))}.isFailure)
   MdmApplyCoordinator(c,port).recover()
   assertEquals("Survived",VpnProfiles.current(c).name);assertEquals(1L,MdmStore(c).read().appliedRevision)
   assertFalse(MdmStore(c).document().has("apply_operation"))
  }finally{c.close()}
 }

}
