package ru.vpnc.quiclab
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Test
import org.junit.Assert.*
import org.junit.runner.RunWith
import org.json.JSONObject
@RunWith(AndroidJUnit4::class)
class MdmProfileSelectionTest {
 private fun runtime(current:String="b")=JSONObject("""{"preferences":{"vpn_profiles":{"current":"$current","multiple":false}},"serverSelectedProfile":"a","profileSelection":{"enabled":true,"profileIds":["a","b"],"serverRevision":1}}""")
 private fun doc()=JSONObject("""{"currentProfileId":"a","profileSelection":{"enabled":true,"profileIds":["a","b"],"serverRevision":1}}""")
 @Test fun preservesLocalSelection(){val next=runtime("a");MdmProfileSelection.carry(doc(),runtime(),next);assertEquals("b",next.getJSONObject("preferences").getJSONObject("vpn_profiles").getString("current"))}
 @Test fun explicitServerSelectionWins(){val d=doc();d.getJSONObject("profileSelection").put("serverRevision",2);val next=runtime("a");MdmProfileSelection.carry(d,runtime(),next);assertEquals("a",next.getJSONObject("preferences").getJSONObject("vpn_profiles").getString("current"))}
 @Test fun revocationRejectsLocalSelection(){val d=doc();d.getJSONObject("profileSelection").put("enabled",false);val next=runtime("a");MdmProfileSelection.carry(d,runtime(),next);assertFalse(MdmProfileSelection.allows(next,"b"));assertEquals("a",next.getJSONObject("preferences").getJSONObject("vpn_profiles").getString("current"))}
 @Test fun rejectsUnknownAndMultiple(){val r=runtime();assertFalse(MdmProfileSelection.allows(r,"unknown"));r.getJSONObject("preferences").getJSONObject("vpn_profiles").put("multiple",true);assertFalse(MdmProfileSelection.allows(r,"a"));assertFalse(MdmProfileSelection.allows(null,"a"))}

 @Test fun encryptedSelectionSurvivesSyncAndRestart(){
  val c=androidx.test.platform.app.InstrumentationRegistry.getInstrumentation().targetContext
  val second="11111111-1111-4111-8111-111111111111"
  val d=JSONObject("""{"schema":1,"profiles":[{"id":"default","name":"First","settings":{"transport":"quic"}},{"id":"$second","name":"Second","settings":{"transport":"https"}}],"currentProfileId":"default","enabledProfileIds":["default"],"multiple":false,"globalApps":[],"dns":{"mode":"tunnel","profileId":"default"},"budget":{"limitBytes":0},"diagnostics":{"enabled":true,"detailed":false},"profileSelection":{"enabled":true,"profileIds":["default","$second"],"serverRevision":1}}""")
  for(mode in listOf("current","external")){
   val file=java.io.File(c.noBackupFilesDir,"selection-test-"+System.nanoTime());val alias="selection-test-"+System.nanoTime()
   try{
    val store=MdmConfigurationStore(file,alias)
    val initial=MdmConfiguration.compile(d,JSONObject("""{"preferences":{},"identities":{}}"""))
    store.initialize(d,initial);store.apply("binding",MdmConfigRevision(1,mode,d),MdmConfiguration::compile)
    store.selectManagedProfile(second)
    val reopened=MdmConfigurationStore(file,alias)
    assertEquals(second,reopened.runtime()!!.getJSONObject("preferences").getJSONObject("vpn_profiles").getString("current"))
    assertFalse(reopened.apply("binding",MdmConfigRevision(1,mode,d),MdmConfiguration::compile))
    assertTrue(reopened.apply("binding",MdmConfigRevision(2,mode,d),MdmConfiguration::compile))
    assertEquals(second,reopened.runtime()!!.getJSONObject("preferences").getJSONObject("vpn_profiles").getString("current"))
    val explicit=JSONObject(d.toString());explicit.getJSONObject("profileSelection").put("serverRevision",2)
    reopened.apply("binding",MdmConfigRevision(3,mode,explicit),MdmConfiguration::compile)
    assertEquals("default",reopened.runtime()!!.getJSONObject("preferences").getJSONObject("vpn_profiles").getString("current"))
    val revoked=JSONObject(explicit.toString());revoked.getJSONObject("profileSelection").put("enabled",false)
    reopened.apply("binding",MdmConfigRevision(4,mode,revoked),MdmConfiguration::compile)
    assertTrue(runCatching{reopened.selectManagedProfile(second)}.isFailure)
    reopened.apply("binding",MdmConfigRevision(5,mode,d),MdmConfiguration::compile)
    reopened.selectManagedProfile(second);reopened.detach()
    assertFalse("permission survived detach",MdmProfileSelection.allows(reopened.runtime(),second))
   }finally{file.delete();java.io.File(file.path+".bak").delete();java.io.File(file.path+".new").delete();java.security.KeyStore.getInstance("AndroidKeyStore").apply{load(null);deleteEntry(alias)}}
  }
 }
}
