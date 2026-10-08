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
class MdmDeviceReportTest {
 private class Isolated(base:Context):ContextWrapper(base){
  val prefix="mdm-report-"+UUID.randomUUID();val names=mutableSetOf<String>();val dir=File(base.cacheDir,prefix).apply{mkdirs()}
  var inventoryCheck:(()->Unit)?=null
  override fun getPackageManager():android.content.pm.PackageManager{inventoryCheck?.invoke();return super.getPackageManager()}
  override fun getFilesDir()=dir
  override fun getNoBackupFilesDir()=File(dir,"private").apply{mkdirs()}
  override fun getSharedPreferences(name:String,mode:Int):SharedPreferences{names.add(prefix+name);return baseContext.getSharedPreferences(prefix+name,mode)}
  fun close(){names.forEach{baseContext.deleteSharedPreferences(it)};dir.deleteRecursively()}
 }
 @Test fun reportRequiresConfigAndNeverExportsIdentity(){
  val c=Isolated(InstrumentationRegistry.getInstrumentation().targetContext)
  try {
   VpnProfiles.list(c)
   VpnIdentity.writeBundle(c,"default",JSONObject().put("key","DO-NOT-EXPORT").put("update_token","SECRET-TOKEN"))
   val store=MdmStore(c);store.edit{it.put("binding",MdmStore.bindingJson(MdmBinding("binding","https://mdm.example",1,MdmRights(),true))).put("active",true)}
   val denied=MdmDeviceReport.snapshot(c,store.read());assertFalse(denied.has("configuration"));assertFalse(denied.has("inventory"));assertFalse(denied.has("vpnState"))
   store.edit{it.put("rights",MdmRights(config=true).json())}
   val allowed=MdmDeviceReport.snapshot(c,store.read())
   assertTrue(allowed.has("configuration"));assertTrue(allowed.has("inventory"))
   assertFalse(allowed.toString().contains("DO-NOT-EXPORT"));assertFalse(allowed.toString().contains("SECRET-TOKEN"))
   val same=MdmDeviceReport.snapshot(c,store.read());assertEquals(allowed.getLong("configGeneration"),same.getLong("configGeneration"))
   assertTrue(same.getLong("sequence")>allowed.getLong("sequence"))
  }finally{c.close()}
 }
 @Test fun packageInventoryDoesNotHoldConfigurationLock(){
  val c=Isolated(InstrumentationRegistry.getInstrumentation().targetContext)
  try{
   VpnProfiles.list(c)
   val store=MdmStore(c);store.edit{it.put("binding",MdmStore.bindingJson(MdmBinding("binding","https://mdm.example",1,MdmRights(),true))).put("active",true).put("rights",MdmRights(config=true).json())}
   var checked=false
   c.inventoryCheck={checked=true;assertFalse("Slow package inventory must not block UI preference reads",Thread.holdsLock(MdmConfiguration.lock))}
   MdmDeviceReport.snapshot(c,store.read())
   assertTrue(checked)
  }finally{c.close()}
 }

 @Test fun consentChangeDuringInventoryDiscardsReport(){
  val c=Isolated(InstrumentationRegistry.getInstrumentation().targetContext)
  try{
   VpnProfiles.list(c)
   val store=MdmStore(c);store.edit{it.put("binding",MdmStore.bindingJson(MdmBinding("binding","https://mdm.example",1,MdmRights(),true))).put("active",true).put("rights",MdmRights(config=true).json())}
   var sent=false
   val controller=MdmController(store,{object:MdmGateway{
    override fun close(){}
    override fun post(b:MdmBinding,op:String,body:JSONObject,secret:String):JSONObject{sent=true;return JSONObject()}
   }},{})
   c.inventoryCheck={store.edit{it.put("generation",it.getLong("generation")+1).put("rights",MdmRights().json())}}
   assertFalse(controller.report{MdmDeviceReport.snapshot(c,it)})
   assertFalse("Report collected before consent revocation must not leave the device",sent)
  }finally{c.close()}
 }

}
