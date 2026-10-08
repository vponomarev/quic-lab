package ru.vpnc.quiclab
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File

class MdmTelemetryTest {
 @Test fun deliveryDefaultsAllowCellButWifiOnlyNeverDoes(){
  assertTrue(MdmTelemetryPolicy.allows(false,false,false))
  assertFalse(MdmTelemetryPolicy.allows(true,false,false))
  assertTrue(MdmTelemetryPolicy.allows(true,true,false))
  assertTrue(MdmTelemetryPolicy.allows(true,true,true))
 }
 @Test fun consentRequiresActiveBindingAndBothRights(){
  val b=MdmBinding("id","https://example.org",1,MdmRights(),true)
  val s=MdmState(b,true,1,MdmRights(telemetry=true,geo=true))
  assertTrue(MdmTelemetryPolicy.consented(s))
  assertFalse(MdmTelemetryPolicy.consented(s.copy(active=false)))
  assertFalse(MdmTelemetryPolicy.consented(s.copy(rights=MdmRights(telemetry=true))))
  assertFalse(MdmTelemetryPolicy.consented(s.copy(cleanupPending=true)))
 }
 @Test fun durableQueueDeduplicatesAcknowledgesAndExpires(){
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val dir=File(c.noBackupFilesDir,"mdm-radio-test-"+System.nanoTime())
  val now=System.currentTimeMillis()
  try{
   val q=MdmTelemetryStore(dir)
   val sample=JSONObject().put("id","one").put("measuredAt",java.time.Instant.ofEpochMilli(now).toString())
   q.append(sample,now);q.append(sample,now)
   assertEquals(1,MdmTelemetryStore(dir).pending(now).length())
   q.acknowledge(setOf("different"),now);assertEquals(1,q.pending(now).length())
   q.acknowledge(setOf("one"),now);assertEquals(0,q.pending(now).length())
   q.append(sample,now)
   assertEquals(0,q.pending(now+24*3600_000L+1).length())
   assertEquals(1L,q.dropped())
  }finally{dir.deleteRecursively()}
 }
 @Test fun queueEraseRemovesUnsentLocationData(){
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val dir=File(c.noBackupFilesDir,"mdm-radio-erase-"+System.nanoTime())
  try{
   val q=MdmTelemetryStore(dir);val now=System.currentTimeMillis()
   q.append(JSONObject().put("id","s").put("measuredAt",java.time.Instant.ofEpochMilli(now).toString()),now)
   q.erase()
   assertEquals(0,MdmTelemetryStore(dir).pending(now).length())
  }finally{dir.deleteRecursively()}
 }

 @Test fun newBindingDoesNotInheritServerCollectionPolicy(){
  val base=InstrumentationRegistry.getInstrumentation().targetContext
  val tag="mdm-scope-"+System.nanoTime();val dir=File(base.noBackupFilesDir,tag).apply{mkdirs()}
  val c=object:android.content.ContextWrapper(base){
   override fun getNoBackupFilesDir()=dir
   override fun getSharedPreferences(n:String,m:Int)=base.getSharedPreferences(tag+n,m)
  }
  val store=MdmStore(c)
  fun bind(id:String){store.edit{it.put("binding",MdmStore.bindingJson(MdmBinding(id,"https://example.org",1,MdmRights(),true))).put("active",true).put("rights",MdmRights(telemetry=true,geo=true).json())}}
  try{
   bind("A");MdmTelemetry.setServerEnabled(c,true);assertTrue(MdmTelemetry.serverEnabled(c))
   bind("B");assertFalse("policy from A leaked into B",MdmTelemetry.serverEnabled(c))
  }finally{dir.deleteRecursively();c.getSharedPreferences("mdm-radio-options",0).edit().clear().commit()}
 }
}
