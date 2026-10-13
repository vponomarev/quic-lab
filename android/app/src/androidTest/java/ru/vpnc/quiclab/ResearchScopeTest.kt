package ru.vpnc.quiclab

import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File

class ResearchScopeTest {
 @Test fun returningToScopeRefreshesConsentWithoutLosingSelection(){
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext
  val before=MdmStore(c).read()
  inst.uiAutomation.executeShellCommand("am start -W -n ru.vpnc.quiclab/.MainActivity").use{android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()}
  val activity=inst.startActivitySync(android.content.Intent(c,ResearchScopeActivity::class.java).addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK))
  fun views(v:android.view.View):List<android.view.View> = listOf(v)+(if(v is android.view.ViewGroup)(0 until v.childCount).flatMap{views(v.getChildAt(it))} else emptyList())
  try {inst.runOnMainSync{
   val all=views(activity.window.decorView)
   val checkbox=all.filterIsInstance<android.widget.CheckBox>().single{it.text.toString()=="Анализ трафика вне VPN"}
   val spinner=all.filterIsInstance<android.widget.Spinner>().single()
   val expected=if(before.binding!=null)before.rights.researchDirect else before.standaloneResearchDirect
   spinner.setSelection(2);checkbox.isChecked=!expected
   inst.callActivityOnPause(activity);inst.callActivityOnResume(activity)
   assertEquals("Consent must refresh on return from MDM",expected,checkbox.isChecked)
   assertEquals("Keep unsaved scope selection",2,spinner.selectedItemPosition)
  };assertEquals(before,MdmStore(c).read())}finally{inst.runOnMainSync{activity.finish()}}
 }

 @Test fun scopeScreenRendersWithoutChangingConsent(){
  org.junit.Assume.assumeTrue(InstrumentationRegistry.getArguments().getString("research_ui")=="true")
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext;val before=MdmStore(c).read()
  inst.uiAutomation.executeShellCommand("am start -W -n ru.vpnc.quiclab/.MainActivity").use{android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()}
  inst.runOnMainSync{c.startActivity(android.content.Intent(c,ResearchScopeActivity::class.java).addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK))}
  val deadline=android.os.SystemClock.elapsedRealtime()+10000
  while(android.os.SystemClock.elapsedRealtime()<deadline && inst.uiAutomation.rootInActiveWindow?.findAccessibilityNodeInfosByText("Охват исследования").isNullOrEmpty())android.os.SystemClock.sleep(100)
  assertFalse("Research screen did not become visible",inst.uiAutomation.rootInActiveWindow?.findAccessibilityNodeInfosByText("Охват исследования").isNullOrEmpty())
  android.os.SystemClock.sleep(600) // Let the activity transition finish before visual inspection.
  val screenshot=inst.uiAutomation.takeScreenshot()
  java.io.File(c.cacheDir,"research-scope-ui.png").outputStream().use{out->screenshot.compress(android.graphics.Bitmap.CompressFormat.PNG,100,out)}
  screenshot.recycle()

  assertEquals(before,MdmStore(c).read())
 }

 @Test fun initialSubscriptionReadsLatestState(){
  var granted=true;var published=true
  granted=false // revoke before the collector subscribes
  val subscription=ResearchSettings.observeCurrent{published=granted}
  try {assertFalse(published);granted=true;ResearchSettings.changed();assertTrue(published)}finally{subscription.close()}
 }

 @Test fun captureUnionDoesNotOverwriteVpnSelection(){
  val vpn=setOf("app.vpn")
  val extra=ResearchScope("vpn_plus_apps",setOf("app.direct"))
  val selected=ResearchCapturePlan.build(1,vpn,extra)
  assertEquals(setOf("app.vpn","app.direct"),selected.allowedApps)
  assertEquals(vpn,selected.vpnApps)
  assertNull(ResearchCapturePlan.build(1,vpn,ResearchScope("all_apps")).allowedApps)
  val excluded=ResearchCapturePlan.build(2,setOf("app.direct","app.other"),extra)
  assertEquals(setOf("app.other"),excluded.excludedApps)
  assertTrue(ResearchCapturePlan.build(3,emptySet(),extra).allRoutes)
  assertFalse(ResearchCapturePlan.build(3,emptySet(),ResearchScope()).allRoutes)
 }
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
