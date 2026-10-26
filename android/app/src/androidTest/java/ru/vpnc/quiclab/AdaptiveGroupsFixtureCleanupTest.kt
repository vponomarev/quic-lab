package ru.vpnc.quiclab

import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.assertTrue
import org.junit.Assume.assumeTrue
import org.junit.Test

/** One-off repair, opt-in only. Never included in ordinary instrumentation runs. */
class AdaptiveGroupsFixtureCleanupTest {
 @Test fun removeOnlyExactUnreferencedSyntheticIdentities(){
  val instrumentation=InstrumentationRegistry.getInstrumentation()
  assumeTrue(InstrumentationRegistry.getArguments().getString("cleanupAdaptiveGroupFixtures")=="true")
  val c=instrumentation.targetContext
  synchronized(MdmConfiguration.lock){
   val runtime=MdmConfigurationStore(c).runtime() ?: return@synchronized
   val identities=runtime.getJSONObject("identities")
   val profiles=VpnProfiles.list(c).map{it.id}.toSet()
   val member="11111111-1111-4111-8111-111111111111"
   val exit="22222222-2222-4222-8222-222222222222"
   val uri="vless://11111111-1111-4111-8111-111111111111@b1.example:443?security=tls&type=tcp&sni=example.com"
   fun exactFixture(id:String,payload:JSONObject):Boolean {
    val keys=payload.keys().asSequence().toSet()
    return when(id){
     member->keys==setOf("vless_uris","vless_config") && payload.optJSONArray("vless_uris")?.let{it.length()==1 && it.optString(0)==uri}==true && payload.optString("vless_config")==mobile.Mobile.importVLESSConfig(uri)
     exit->keys==setOf("certificate","key") && payload.optString("certificate")=="cert" && payload.optString("key")=="key"
     else->false
    }
   }
   var removed=0
   for(id in listOf(member,exit)){
    val payload=identities.optJSONObject(id) ?: continue
    // A real configured profile must be recovered from its backup, never deleted here.
    assertTrue("Synthetic ID is referenced by a real profile; restore from backup",id !in profiles)
    assertTrue("Identity does not exactly match the synthetic fixture; unchanged",exactFixture(id,payload))
   }
   for(id in listOf(member,exit))if(identities.has(id)){
    assertTrue(MdmConfiguration.deleteIdentity(c,id))
    assertTrue(MdmConfiguration.identity(c,id)==null)
    removed++
   }
   android.util.Log.i("AdaptiveGroupsCleanup","Removed exact synthetic identities: $removed; profiles/settings unchanged")
  }
 }
}
