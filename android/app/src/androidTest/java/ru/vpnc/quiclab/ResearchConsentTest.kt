package ru.vpnc.quiclab

import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File

class ResearchConsentTest {
 @Test fun oldRightsAndOtherConsentNeverGrantResearch(){
  assertFalse(MdmRights.parse(JSONObject().put("telemetry",true).put("geo",true)).research)
  assertTrue(MdmRights.parse(MdmRights(research=true).json()).research)
 }
 @Test fun authorityRequiresActiveBindingAndSeparateGrant(){
  val binding=MdmBinding("test","https://mdm.example",1,MdmRights(),true)
  val local=MdmState(null,false,0,MdmRights(),standaloneResearch=true)
  assertTrue(ResearchSettings.allowed(local))
  val managed=local.copy(binding=binding,active=true)
  assertFalse(ResearchSettings.allowed(managed))
  val granted=managed.copy(rights=MdmRights(research=true))
  assertTrue(ResearchSettings.allowed(granted))
  assertFalse(ResearchSettings.allowed(granted.copy(active=false)))
  assertFalse(ResearchSettings.allowed(granted.copy(cleanupPending=true)))
  assertFalse(ResearchSettings.allowed(local.copy(pendingEnrollment=true)))
 }
 @Test fun enrollmentPauseDeleteCannotResurrectStandaloneConsent(){
  val ctx=InstrumentationRegistry.getInstrumentation().targetContext
  val file=File(ctx.noBackupFilesDir,"research-consent-"+System.nanoTime())
  val alias=file.name
  val store=MdmStore(file,alias)
  var changes=0
  var epoch=0L
  val controller=MdmController(store,{object:MdmGateway {
   override fun close(){}
   override fun post(b:MdmBinding,op:String,body:JSONObject,secret:String)=JSONObject()
    .put("id","test").put("epoch",if(op=="activate")++epoch else epoch)
    .put("active",op=="activate").put("requestedRights",MdmRights(research=true).json())
  }},{},stateChanged={changes++})
  try{
   assertFalse(ResearchSettings.allowed(store.read()))
   store.setStandaloneResearch(true)
   assertTrue(ResearchSettings.allowed(store.read()))
   controller.enroll(MdmInvitation("https://mdm.example","a".repeat(64),MdmRights(research=true)),MdmRights(research=true))
   assertFalse(store.read().standaloneResearch)
   assertTrue(ResearchSettings.allowed(store.read()))
   assertTrue(runCatching{store.setStandaloneResearch(true)}.isFailure)
   controller.setRights(MdmRights(telemetry=true))
   assertFalse(ResearchSettings.allowed(store.read()))
   controller.setRights(MdmRights(research=true))
   controller.pause()
   assertFalse(ResearchSettings.allowed(store.read()))
   controller.delete()
   assertFalse(ResearchSettings.allowed(MdmStore(file,alias).read()))
   assertTrue(changes>=6)
   store.setStandaloneResearch(true)
   assertTrue(ResearchSettings.allowed(store.read()))
  }finally{
   file.delete()
   java.security.KeyStore.getInstance("AndroidKeyStore").apply{load(null);deleteEntry(alias)}
  }
 }
}
