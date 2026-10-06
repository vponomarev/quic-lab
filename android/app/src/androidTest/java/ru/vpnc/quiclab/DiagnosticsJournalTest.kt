package ru.vpnc.quiclab
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test
import java.io.File
class DiagnosticsJournalTest {
 @Test fun retentionAckAndIsolation() {
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val db=DiagnosticsJournal(c,"diagnostics-test-"+System.nanoTime()+".db")
  try {
   db.append("p","https://one.test", "event","connected",1000)
   db.append("q","https://two.test","event","other",1000)
   val rows=db.pending("p","https://one.test");assertEquals(1,rows.size)
   assertEquals(0,db.pending("p","https://two.test").size)
   db.ack(rows.map{it.id});assertEquals(0,db.pending("p","https://one.test").size)
   assertEquals(2,db.recent().size)
   db.prune(1000+8*86400000L);assertEquals(0,db.recent().size)
  } finally {db.close()}
 }
 @Test fun consentAndNetworkPolicy() {
  assertTrue(DiagnosticsPolicy.autoAllowed(true,true,true,true))
  assertFalse(DiagnosticsPolicy.autoAllowed(true,true,false,true))
  assertFalse(DiagnosticsPolicy.autoAllowed(false,true,true,true))
  assertFalse(DiagnosticsPolicy.autoAllowed(true,false,true,true))
  assertFalse(DiagnosticsPolicy.autoAllowed(true,true,true,false))
 }

 @Test fun collectionToggleAndDetailedOptIn(){
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  Diagnostics.init(c);val prefs=DiagnosticsPolicy.preferences(c)
  val hadEnabled=prefs.contains("enabled");val enabled=prefs.getBoolean("enabled",true)
  val hadDetail=prefs.contains("detailed");val detailed=prefs.getBoolean("detailed",false)
  val marker="diag-test-"+System.nanoTime()
  try{
   prefs.edit().putBoolean("enabled",false).commit()
   Diagnostics.event("test",org.json.JSONObject().put("event",marker+"-off"))
   assertFalse(Diagnostics.store().recent().any{it.text.contains(marker)})
   prefs.edit().putBoolean("enabled",true).putBoolean("detailed",false).commit()
   Diagnostics.event("test",org.json.JSONObject().put("event","flow_open").put("destination",marker))
   assertFalse(Diagnostics.store().recent().any{it.text.contains(marker)})
   Diagnostics.event("test",org.json.JSONObject().put("event",marker+"-on"))
   assertTrue(Diagnostics.store().recent().any{it.text.contains(marker+"-on")})
   prefs.edit().putBoolean("detailed",true).commit()
   Diagnostics.event("test",org.json.JSONObject().put("event","flow_open").put("destination",marker))
   assertTrue(Diagnostics.store().recent().any{it.kind=="detail"&&it.text.contains(marker)})
  }finally{
   prefs.edit().apply{if(hadEnabled)putBoolean("enabled",enabled)else remove("enabled");if(hadDetail)putBoolean("detailed",detailed)else remove("detailed")}.commit()
  }
 }
 @Test fun deliveryLive(){
  org.junit.Assume.assumeTrue(InstrumentationRegistry.getArguments().getString("diagnostics_live")=="true")
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  Diagnostics.init(c)
  val fixture=File(c.filesDir,"diagnostics-live-identity.json")
  val identity=org.json.JSONObject(fixture.readText())
  val profile=java.util.UUID.randomUUID().toString()
  val badProfile=java.util.UUID.randomUUID().toString()
  try{
   VpnIdentity.writeBundle(c,badProfile,org.json.JSONObject(identity.toString()).put("update_token","a".repeat(64)))
   VpnIdentity.writeBundle(c,profile,identity)
   val bad=DiagnosticsDelivery.destination(c,badProfile)
   Diagnostics.store().append(badProfile,bad.first,"event","diagnostics_rejected_test")
   val destination=DiagnosticsDelivery.destination(c,profile)
   assertTrue(destination.first.startsWith("https://"))
   Diagnostics.store().append(profile,destination.first,"event","diagnostics_live_test")
   val done=java.util.concurrent.CountDownLatch(1)
   DiagnosticsDelivery.manual(c){done.countDown()}
   assertTrue("Upload timed out",done.await(90,java.util.concurrent.TimeUnit.SECONDS))
   val prefs=DiagnosticsPolicy.preferences(c)
   assertTrue(prefs.getString("status",""),Diagnostics.store().pending(profile,destination.first).isEmpty())
   assertFalse("Wrong-token batch must remain unacknowledged",Diagnostics.store().pending(badProfile,bad.first).isEmpty())
   assertTrue(prefs.getLong("last_sent",0)>0)
  }finally{
   for(id in listOf(profile,badProfile)){VpnProfiles.identityFile(c,id).delete();Diagnostics.store().writableDatabase.delete("records","profile=?",arrayOf(id))}
   fixture.delete()
  }
 }

 @Test fun staleOutboxExpiresWithoutNetwork(){
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val dir=File(c.filesDir,"diagnostic-outbox").apply{mkdirs()}
  val f=File(dir,"retention-test.json")
  val record=org.json.JSONObject().put("id","old").put("kind","detail").put("time",System.currentTimeMillis()-2*86400000L).put("text","old")
  f.writeText(org.json.JSONObject().put("version",1).put("records",org.json.JSONArray().put(record)).toString())
  DiagnosticsDelivery.pruneOutbox(c);assertFalse(f.exists())
 }

 @Test fun settingsScreenAndVpnSmoke(){
  org.junit.Assume.assumeTrue(InstrumentationRegistry.getArguments().getString("diagnostics_ui")=="true")
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext
  inst.uiAutomation.executeShellCommand("am start -W -a android.intent.action.MAIN -c android.intent.category.LAUNCHER -n ru.vpnc.quiclab/.MainActivity").use{android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()}
  Thread.sleep(700)
  inst.runOnMainSync{
   val a=androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(androidx.test.runner.lifecycle.Stage.RESUMED).filterIsInstance<android.app.Activity>().first()
   a.startActivity(android.content.Intent(a,DiagnosticsSettingsActivity::class.java))
  }
  Thread.sleep(700)
  inst.runOnMainSync{
   val a=androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(androidx.test.runner.lifecycle.Stage.RESUMED).filterIsInstance<DiagnosticsSettingsActivity>().first()
   fun views(v:android.view.View):List<android.view.View> = listOf(v)+(if(v is android.view.ViewGroup)(0 until v.childCount).flatMap{views(v.getChildAt(it))}else emptyList())
   val all=views(a.window.decorView)
   assertEquals(2,all.filterIsInstance<android.widget.Switch>().size)
   assertTrue(all.filterIsInstance<android.widget.Button>().any{it.text.toString()=="Отправить сейчас"})
   assertTrue(all.filterIsInstance<android.widget.TextView>().any{it.text.toString().contains("Получатели:")})
   a.finish()
  }
  assertNull(android.net.VpnService.prepare(c))
  c.startForegroundService(android.content.Intent(c,LabVpnService::class.java))
  val until=android.os.SystemClock.elapsedRealtime()+45000
  while(android.os.SystemClock.elapsedRealtime()<until && !(LabVpnService.active&&LabVpnService.lastTransitEcho>0&&android.os.SystemClock.elapsedRealtime()-LabVpnService.lastTransitEcho<3000))Thread.sleep(250)
  assertTrue("VPN must run",LabVpnService.active)
  assertTrue("Transit through VPN must reply: "+LabVpnService.status,LabVpnService.lastTransitEcho>0&&android.os.SystemClock.elapsedRealtime()-LabVpnService.lastTransitEcho<3000)
 }
}
