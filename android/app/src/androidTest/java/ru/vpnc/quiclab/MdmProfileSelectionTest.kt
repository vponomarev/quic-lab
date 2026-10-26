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
 @Test fun preservesDelegatedTransportOnUnchangedServerSync(){
  val old=runtime();val next=runtime("a")
  for(r in listOf(old,next)){
   r.getJSONObject("preferences").put("vpn_b",JSONObject("""{"transport":"quic","endpoint":"host:443","quic_endpoint":"host:443","https_endpoint":"host:8443","available_transports":["quic","https"]}"""))
   r.put("identities",JSONObject().put("b",JSONObject().put("certificate","cert").put("key","key")))
  }
  old.put("localTransports",JSONObject().put("b",JSONObject().put("transport","https").put("baseline",JSONObject(next.getJSONObject("preferences").getJSONObject("vpn_b").toString())).put("identity",JSONObject(next.getJSONObject("identities").getJSONObject("b").toString()))))
  old.getJSONObject("preferences").getJSONObject("vpn_b").put("transport","https").put("endpoint","host:8443")
  MdmProfileSelection.carry(doc(),old,next)
  assertEquals("https",next.getJSONObject("preferences").getJSONObject("vpn_b").getString("transport"))
  assertEquals("host:8443",next.getJSONObject("preferences").getJSONObject("vpn_b").getString("endpoint"))
 }
 private fun transportRuntime():JSONObject=runtime().apply{
  getJSONObject("preferences").put("vpn_b",JSONObject("""{"transport":"quic","endpoint":"host:443","quic_endpoint":"host:443","https_endpoint":"host:8443","awg_endpoint":"host:51820","available_transports":["quic","https","awg"]}"""))
  put("identities",JSONObject().put("b",JSONObject().put("certificate","cert").put("key","key").put("awg_config","provisioned")))
 }
 @Test fun canReturnToOriginalTransportWithOnlyGenericEndpoint(){
  val r=transportRuntime();r.getJSONObject("preferences").getJSONObject("vpn_b").remove("quic_endpoint")
  MdmProfileSelection.selectTransport(r,"b","https")
  assertTrue(MdmProfileSelection.transports(r,"b").contains("quic"))
  MdmProfileSelection.selectTransport(r,"b","quic")
  assertEquals("host:443",r.getJSONObject("preferences").getJSONObject("vpn_b").getString("endpoint"))
 }
 @Test fun delegatedTransportRejectsMissingCredentialsAndUnlistedProfiles(){
  val r=transportRuntime()
  assertEquals(listOf("quic","https","awg"),MdmProfileSelection.transports(r,"b"))
  MdmProfileSelection.selectTransport(r,"b","awg")
  assertEquals("host:51820",r.getJSONObject("preferences").getJSONObject("vpn_b").getString("endpoint"))
  r.getJSONObject("identities").getJSONObject("b").remove("awg_config")
  assertFalse(MdmProfileSelection.transports(r,"b").contains("awg"))
  assertTrue(runCatching{MdmProfileSelection.selectTransport(r,"b","awg")}.isFailure)
  assertTrue(runCatching{MdmProfileSelection.selectTransport(r,"unknown","https")}.isFailure)
  assertTrue(runCatching{MdmProfileSelection.selectTransport(r,"b","vless")}.isFailure)
  r.getJSONObject("profileSelection").put("enabled",false)
  assertTrue(runCatching{MdmProfileSelection.selectTransport(r,"b","https")}.isFailure)
 }
 @Test fun serverTransportCredentialsAndPolicyChangesOverrideLocalChoice(){
  val old=transportRuntime();MdmProfileSelection.selectTransport(old,"b","https")
  for(change in 0..3){
   val next=transportRuntime();val d=doc()
   when(change){
    0->next.getJSONObject("preferences").getJSONObject("vpn_b").put("https_endpoint","new:443")
    1->next.getJSONObject("identities").getJSONObject("b").put("key","rotated")
    2->d.getJSONObject("profileSelection").put("serverRevision",2)
    3->d.getJSONObject("profileSelection").put("enabled",false)
   }
   MdmProfileSelection.carry(d,old,next)
   assertEquals("case $change","quic",next.getJSONObject("preferences").getJSONObject("vpn_b").getString("transport"))
   assertFalse(next.has("localTransports"))
  }
 }
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
    initial.getJSONObject("identities").put(second,JSONObject().put("certificate","cert").put("key","key"))
    d.getJSONArray("profiles").getJSONObject(1).getJSONObject("settings").put("quic_endpoint","host:443").put("https_endpoint","host:8443").put("endpoint","host:8443")
    store.initialize(d,initial);store.apply("binding",MdmConfigRevision(1,mode,d),MdmConfiguration::compile)
    store.selectManagedProfile(second)
    store.selectManagedTransport(second,"quic")
    val reopened=MdmConfigurationStore(file,alias)
    assertEquals(second,reopened.runtime()!!.getJSONObject("preferences").getJSONObject("vpn_profiles").getString("current"))
    assertEquals("quic",reopened.runtime()!!.getJSONObject("preferences").getJSONObject("vpn_$second").getString("transport"))
    assertFalse(reopened.apply("binding",MdmConfigRevision(1,mode,d),MdmConfiguration::compile))
    assertTrue(reopened.apply("binding",MdmConfigRevision(2,mode,d),MdmConfiguration::compile))
    assertEquals(second,reopened.runtime()!!.getJSONObject("preferences").getJSONObject("vpn_profiles").getString("current"))
    assertEquals("quic",reopened.runtime()!!.getJSONObject("preferences").getJSONObject("vpn_$second").getString("transport"))
    val explicit=JSONObject(d.toString());explicit.getJSONObject("profileSelection").put("serverRevision",2)
    reopened.apply("binding",MdmConfigRevision(3,mode,explicit),MdmConfiguration::compile)
    assertEquals("default",reopened.runtime()!!.getJSONObject("preferences").getJSONObject("vpn_profiles").getString("current"))
    assertEquals("https",reopened.runtime()!!.getJSONObject("preferences").getJSONObject("vpn_$second").getString("transport"))
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
