package ru.vpnc.quiclab

import android.app.job.JobInfo
import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File
import java.util.UUID
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

class MdmQuietTest {
 private class Isolated(base:Context):ContextWrapper(base){
  private val prefix="quiet-mdm-${UUID.randomUUID()}"
  private val names=mutableSetOf<String>()
  private val dir=File(base.cacheDir,prefix).apply{check(mkdirs())}
  var inventoryCheck:(()->Unit)?=null
  override fun getPackageManager():android.content.pm.PackageManager{inventoryCheck?.invoke();return super.getPackageManager()}
  override fun getFilesDir()=dir
  override fun getNoBackupFilesDir()=dir
  override fun getApplicationContext():Context=this
  override fun getSharedPreferences(name:String,mode:Int):SharedPreferences{names.add(prefix+name);return baseContext.getSharedPreferences(prefix+name,mode)}
  fun close(){names.forEach{baseContext.deleteSharedPreferences(it)};check(dir.parentFile==baseContext.cacheDir);dir.deleteRecursively()}
 }
 private fun fixture(block:(Isolated)->Unit){val c=Isolated(InstrumentationRegistry.getInstrumentation().targetContext);try{block(c)}finally{c.close()}}
 private fun active(c:Context){MdmStore(c).edit{it.put("binding",MdmStore.bindingJson(MdmBinding("quiet-fixture","https://mdm.example",1,MdmRights(),true))).put("active",true).put("generation",1).put("secret","fixture-secret")}}
 @Test fun defaultIsContinuousButQuietChoicePersistsAndVpnOverridesIt()=fixture{c->
  active(c);val state=MdmStore(c).read()
  assertFalse(MdmBackground.quiet(c))
  assertEquals("continuous",MdmBackground.mode(state,false,false))
  MdmBackground.setQuiet(c,true)
  assertTrue(MdmBackground.quiet(c))
  assertEquals("quiet",MdmBackground.mode(state,MdmBackground.quiet(c),false))
  assertEquals("continuous",MdmBackground.mode(state,true,true))
  MdmStore(c).edit{it.put("active",false)}
  assertEquals("off",MdmBackground.mode(MdmStore(c).read(),true,true))
  MdmStore(c).edit{it.put("active",true).put("cleanupPending",true)}
  assertEquals("off",MdmBackground.mode(MdmStore(c).read(),false,true))
 }
 private class Port:MdmBackgroundPort{
  val calls=mutableListOf<String>()
  override fun periodic(){calls.add("periodic")}
  override fun immediate(){calls.add("immediate")}
  override fun cancelJobs(){calls.add("cancelJobs")}
  override fun cancelQuiet(){calls.add("cancelQuiet")}
  override fun startContinuous(){calls.add("start")}
  override fun stopContinuous(){calls.add("stop")}
  override fun stopCollector(){calls.add("stopCollector")}
 }
 @Test fun quietNeverStartsServicesAndOpeningOnlyRequestsOneShot(){
  val p=Port();MdmBackground.reconcile("quiet",false,p)
  assertEquals(listOf("stop","stopCollector","periodic"),p.calls)
  p.calls.clear();MdmBackground.reconcile("quiet",true,p)
  assertEquals(listOf("stop","stopCollector","periodic","immediate"),p.calls)
  p.calls.clear();MdmBackground.reconcile("off",true,p)
  assertEquals(listOf("cancelJobs","cancelQuiet","stop","stopCollector"),p.calls)
  p.calls.clear();MdmBackground.reconcile("continuous",true,p)
  assertEquals(listOf("cancelJobs","cancelQuiet","start"),p.calls)
 }
 @Test fun jobsAreBoundedNetworkWorkWithStableSeparateIds()=fixture{c->
  val periodic=MdmBackground.job(c,true);val immediate=MdmBackground.job(c,false)
  assertEquals(2*60*60*1000L,periodic.intervalMillis)
  assertTrue(periodic.isPeriodic);assertTrue(periodic.isPersisted)
  assertFalse(immediate.isPeriodic);assertFalse(immediate.isPersisted)
  assertNotEquals(periodic.id,immediate.id)
  assertEquals(JobInfo.NETWORK_TYPE_ANY,periodic.networkType)
  assertFalse(periodic.isRequireCharging);assertFalse(periodic.isRequireDeviceIdle)
  assertEquals(90000L,MdmQuietJob.MAX_PASS_MS)
 }
 @Test fun canceledLeaseCannotReleaseOrCancelItsSuccessorBeforeDrain(){
  val gate=MdmSessionGate();val entered=CountDownLatch(1);val release=CountDownLatch(1);var closes=0
  val source=object:MdmSession{
   override fun sync(){entered.countDown();check(release.await(5,TimeUnit.SECONDS))}
   override fun pause(){}
   override fun close(){closes++}
  }
  val first=gate.open{source}!!;val worker=Thread{first.sync()};worker.start()
  try{
   assertTrue(entered.await(5,TimeUnit.SECONDS));first.close()
   assertEquals(1,closes);assertNull(gate.open{source})
  }finally{release.countDown();worker.join(5000)}
  val second=gate.open{source};assertNotNull(second)
  first.close();assertEquals(1,closes)
  second!!.close();assertEquals(2,closes)
 }
 @Test fun quietPassPollsWithoutWaitingReportsAndAcknowledges()=fixture{c->
  active(c);val operations=mutableListOf<String>()
  val controller=MdmController(MdmStore(c),{object:MdmGateway{
   override fun post(b:MdmBinding,op:String,body:JSONObject,secret:String):JSONObject{
    operations.add(op)
    if(op=="sync"){assertFalse(body.getBoolean("wait"));return JSONObject("""{"version":1,"epoch":1,"serverTime":"2026-11-04T00:00:00Z"}""")}
    return JSONObject().put("accepted",true)
   }
   override fun close(){}
  }},{})
  val session=MdmRuntime.session(c,quiet=true,selectedController=controller)!!
  try{session.sync()}finally{session.close()}
  assertEquals(listOf("sync","report","sync"),operations)
 }
 @Test fun oneShotSampleRequiresLiveConsentAndCurrentAndroidPermission(){
  assertFalse(MdmBackground.canSample(false,true,true,true))
  assertFalse(MdmBackground.canSample(true,false,true,true))
  assertFalse(MdmBackground.canSample(true,true,false,false))
  assertTrue(MdmBackground.canSample(true,true,true,false))
  assertTrue(MdmBackground.canSample(true,true,false,true))
 }
 @Test fun quietReportRequiresServerCapabilityAndDoesNotClaimAppliedConfig()=fixture{c->
  active(c);MdmBackground.setQuiet(c,true)
  val old=MdmDeviceReport.snapshot(c,MdmStore(c).read())
  assertFalse(old.has("backgroundMode"))
  MdmStore(c).edit{it.put("server_mdm_quiet_v1",true)}
  val modern=MdmDeviceReport.snapshot(c,MdmStore(c).read())
  assertEquals("quiet",modern.getString("backgroundMode"))
  assertEquals(0L,modern.getLong("appliedRevision"))
  assertTrue(modern.getJSONArray("capabilities").toString().contains("mdm_quiet_v1"))
 }
 @Test fun cancellationDuringPollDoesNotReportOrAcknowledge()=fixture{c->
  active(c);val operations=mutableListOf<String>();lateinit var session:MdmSession
  val controller=MdmController(MdmStore(c),{object:MdmGateway{
   override fun post(b:MdmBinding,op:String,body:JSONObject,secret:String):JSONObject{
    operations.add(op);session.close()
    return JSONObject("""{"version":1,"epoch":1,"serverTime":"2026-11-04T00:00:00Z"}""")
   }
   override fun close(){}
  }},{})
  session=MdmRuntime.session(c,quiet=true,selectedController=controller)!!
  try{assertTrue(runCatching{session.sync()}.isFailure)}finally{session.close()}
  assertEquals(listOf("sync"),operations)
 }
 @Test fun quietPassAppliesBeforeAcknowledgingAndRejectsExpiredCommand()=fixture{c->
  active(c);MdmStore(c).edit{it.put("rights",MdmRights(config=true,vpn=true).json())}
  val document=MdmConfiguration.snapshotLocal(c)
  document.getJSONArray("profiles").getJSONObject(0).put("name","Quiet applied")
  var polls=0;var reports=0
  val controller=MdmController(MdmStore(c),{object:MdmGateway{
   override fun post(b:MdmBinding,op:String,body:JSONObject,secret:String):JSONObject{
    if(op=="report"){reports++;assertEquals(1L,body.getJSONObject("report").getLong("appliedRevision"));return JSONObject().put("accepted",true)}
    assertEquals("sync",op);assertFalse(body.getBoolean("wait"));polls++
    val response=JSONObject("""{"version":1,"epoch":1,"serverTime":"2026-11-04T00:00:00Z"}""")
    if(polls==1){
     response.put("desiredConfig",JSONObject().put("revision",1).put("mode","current").put("document",document))
     response.put("commands",org.json.JSONArray("""[{"id":"expired-test","bindingId":"quiet-fixture","epoch":1,"issuedAt":"2026-11-03T22:00:00Z","expiresAt":"2026-11-03T22:05:00Z","kind":"vpn_start"}]"""))
    }else{
     assertEquals(1L,body.getLong("appliedRevision"))
     assertTrue(body.getJSONArray("events").toString().contains("expired"))
    }
    return response
   }
   override fun close(){}
  }},{})
  val session=MdmRuntime.session(c,quiet=true,selectedController=controller)!!
  try{session.sync()}finally{session.close()}
  assertEquals(2,polls);assertEquals(1,reports)
  assertEquals("Quiet applied",VpnProfiles.current(c).name)
 }
 @Test fun cancellationDuringInventoryCannotOpenNewReportRequest()=fixture{c->
  active(c);MdmStore(c).edit{it.put("rights",MdmRights(config=true).json())}
  val operations=mutableListOf<String>();lateinit var session:MdmSession
  val controller=MdmController(MdmStore(c),{object:MdmGateway{
   override fun post(b:MdmBinding,op:String,body:JSONObject,secret:String):JSONObject{
    operations.add(op);return JSONObject("""{"version":1,"epoch":1,"serverTime":"2026-11-04T00:00:00Z"}""")
   }
   override fun close(){}
  }},{})
  session=MdmRuntime.session(c,quiet=true,selectedController=controller)!!
  c.inventoryCheck={session.close()}
  try{assertTrue(runCatching{session.sync()}.isFailure)}finally{session.close()}
  assertEquals("cancelled inventory opened a new report channel",listOf("sync"),operations)
 }
 @Test fun cancellationDuringGatewayRegistrationCannotSend()=fixture{c->
  active(c);var sends=0;lateinit var session:MdmSession
  val controller=MdmController(MdmStore(c),{
   session.close()
   object:MdmGateway{
    override fun post(b:MdmBinding,op:String,body:JSONObject,secret:String):JSONObject{sends++;return JSONObject("""{"version":1,"epoch":1,"serverTime":"2026-11-04T00:00:00Z"}""")}
    override fun close(){}
   }
  },{})
  session=MdmRuntime.session(c,quiet=true,selectedController=controller)!!
  try{assertTrue(runCatching{session.sync()}.isFailure)}finally{session.close()}
  assertEquals("cancelled session registered and sent a new request",0,sends)
 }
 @Test fun quietSequenceCommandsRejectStartButAllowCancellation()=fixture{c->
  active(c);MdmStore(c).edit{it.put("rights",MdmRights(vpn=true,telemetry=true).json())}
  VlessProbeStore(c).save(JSONObject("""{"schema":2,"runId":"local-sequence","sourceId":"source","fingerprint":"fp","initiator":"local","state":"finished","phases":{"wifi":{"network":"wifi","state":"finished","rows":[]}}}"""))
  var polls=0
  val controller=MdmController(MdmStore(c),{object:MdmGateway{
   override fun post(b:MdmBinding,op:String,body:JSONObject,secret:String):JSONObject{
    if(op=="report")return JSONObject().put("accepted",true)
    polls++
    val response=JSONObject("""{"version":1,"epoch":1,"serverTime":"2026-11-04T00:00:00Z","capabilities":["vless_probe_v1","vless_probe_phases_v2","vless_probe_sequence_v2"]}""")
    if(polls==1){val commands=org.json.JSONArray();for(action in listOf("start","cancel"))commands.put(JSONObject().put("id","sequence-$action").put("bindingId","quiet-fixture").put("epoch",1).put("issuedAt","2026-11-04T00:00:00Z").put("expiresAt","2026-11-04T00:05:00Z").put("kind","vless_probe_sequence").put("probe",JSONObject().put("mode","phased").put("action",action).put("sourceId","source").put("fingerprint","fp").put("runId","local-sequence")));response.put("commands",commands)}
    else {val events=body.getJSONArray("events").toString();assertTrue(events.contains("background_restricted"));assertTrue(events.contains("not_running"))}
    return response
   }
   override fun close(){}
  }},{})
  val session=MdmRuntime.session(c,quiet=true,selectedController=controller)!!
  try{session.sync()}finally{session.close()};assertEquals(2,polls)
 }
 @Test fun quietPassAllowsAuthorizedProbeCancellation()=fixture{c->
  active(c);MdmStore(c).edit{it.put("rights",MdmRights(vpn=true,telemetry=true).json())}
  var polls=0
  val controller=MdmController(MdmStore(c),{object:MdmGateway{
   override fun post(b:MdmBinding,op:String,body:JSONObject,secret:String):JSONObject{
    if(op=="report")return JSONObject().put("accepted",true)
    polls++
    val response=JSONObject("""{"version":1,"epoch":1,"serverTime":"2026-11-04T00:00:00Z","capabilities":["vless_probe_v1"]}""")
    if(polls==1)response.put("commands",org.json.JSONArray("""[{"id":"quiet-cancel","bindingId":"quiet-fixture","epoch":1,"issuedAt":"2026-11-04T00:00:00Z","expiresAt":"2026-11-04T00:05:00Z","kind":"vless_probe","probe":{"action":"cancel","runId":"quiet-no-such-run"}}]"""))
    else assertTrue("quiet mode rejected cancel instead of checking the run",body.getJSONArray("events").toString().contains("not_running"))
    return response
   }
   override fun close(){}
  }},{})
  val session=MdmRuntime.session(c,quiet=true,selectedController=controller)!!
  try{session.sync()}finally{session.close()}
  assertEquals(2,polls)
 }
}
