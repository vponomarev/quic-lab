package ru.vpnc.quiclab

import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File

class ResearchScopeTest {
 @Test fun legacyRightsDoNotGrantDirectResearch(){
  assertFalse(MdmRights.parse(JSONObject().put("research",true)).researchDirect)
  assertTrue(MdmRights.parse(MdmRights(researchDirect=true).json()).researchDirect)
 }
 @Test fun separateConsentAndRevocationPersist(){
  val ctx=InstrumentationRegistry.getInstrumentation().targetContext
  val f=File(ctx.noBackupFilesDir,"research-scope-"+System.nanoTime());val store=MdmStore(f,f.name)
  try{
   assertEquals("vpn_only",store.read().researchScope.mode)
   assertFalse(ResearchSettings.directAllowed(store.read()))
   store.setStandaloneResearch(true);store.setStandaloneResearchDirect(true)
   store.setResearchScope(ResearchScope("vpn_plus_apps",setOf("app.one")))
   assertEquals("vpn_plus_apps",ResearchSettings.effectiveScope(store.read()).mode)
   store.setStandaloneResearchDirect(false)
   assertEquals("vpn_only",ResearchSettings.effectiveScope(store.read()).mode)
   assertEquals(setOf("app.one"),store.read().researchScope.packages)
   store.setStandaloneResearchDirect(true)
   assertEquals("vpn_only",ResearchSettings.effectiveScope(MdmStore(f,f.name).read()).mode)
   store.setResearchScope(ResearchScope("all_apps",setOf("app.one")))
   assertEquals("all_apps",ResearchSettings.effectiveScope(store.read()).mode)
   store.setStandaloneResearch(false)
   assertFalse(ResearchSettings.directAllowed(store.read()))
   assertEquals("vpn_only",ResearchSettings.effectiveScope(store.read()).mode)
  }finally{f.delete();java.security.KeyStore.getInstance("AndroidKeyStore").apply{load(null);deleteEntry(f.name)}}
 }
 @Test fun managedDirectNeedsBothRightsAndActiveAuthority(){
  val b=MdmBinding("test","https://mdm.example",1,MdmRights(),true)
  val s=MdmState(b,true,1,MdmRights(research=true,researchDirect=true),researchScope=ResearchScope("all_apps"))
  assertTrue(ResearchSettings.directAllowed(s))
  assertFalse(ResearchSettings.directAllowed(s.copy(active=false)))
  assertFalse(ResearchSettings.directAllowed(s.copy(cleanupPending=true)))
  assertFalse(ResearchSettings.directAllowed(s.copy(rights=MdmRights(researchDirect=true))))
  assertFalse(ResearchSettings.directAllowed(s.copy(rights=MdmRights(research=true))))
 }
}
