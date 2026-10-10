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
}
