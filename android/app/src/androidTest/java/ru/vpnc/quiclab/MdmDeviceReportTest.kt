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
}
