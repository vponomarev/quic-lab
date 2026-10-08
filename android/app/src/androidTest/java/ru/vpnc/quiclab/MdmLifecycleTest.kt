package ru.vpnc.quiclab
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File

class MdmLifecycleTest {
 private class Server {
  var epoch=0L;var enrollments=0;var registration="";var loseOnce=false;var offline=false
  fun gateway()=object:MdmGateway{
   override fun close(){}
   override fun post(b:MdmBinding,op:String,body:JSONObject,secret:String):JSONObject {
    if(offline)error("offline")
    if(op=="enroll"){
     if(registration.isEmpty()){registration=body.getString("registrationId");enrollments++}
     assertEquals(registration,body.getString("registrationId"))
     if(loseOnce){loseOnce=false;error("response lost")}
    }
    if(op=="activate")epoch++
    return JSONObject().put("id","device").put("epoch",epoch).put("active",op=="activate")
     .put("requestedRights",MdmRights(config=true,vpn=true).json())
   }
  }
 }
 private fun fixture(block:(MdmStore,MdmController,Server)->Unit){
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val f=File(c.noBackupFilesDir,"mdm-life-"+System.nanoTime());val alias="mdm-life-"+System.nanoTime()
  val store=MdmStore(f,alias);val server=Server()
  try{block(store,MdmController(store,{server.gateway()},{}),server)}
  finally{f.delete();java.security.KeyStore.getInstance("AndroidKeyStore").apply{load(null);deleteEntry(alias)}}
 }
 private val invite=MdmInvitation("https://mdm.example","a".repeat(64),MdmRights(config=true,vpn=true))
 @Test fun secondBindingRejectedEvenPaused()=fixture{_,c,_->
  c.enroll(invite,MdmRights(config=true));c.pause()
  assertTrue(runCatching{c.enroll(invite,MdmRights())}.isFailure)
 }
 @Test fun pauseBeforeLateResponseAndResumeInvalidatesOldEpoch()=fixture{_,c,_->
  val before=c.enroll(invite,MdmRights(config=true,vpn=true))
  assertTrue(c.authorize(before.localGeneration,MdmRight.CONFIG))
  c.pause();assertFalse(c.authorize(before.localGeneration,MdmRight.CONFIG))
  val after=c.resume()
  assertTrue(after.binding!!.epoch>before.binding!!.epoch)
  assertFalse(c.authorize(before.localGeneration,MdmRight.CONFIG))
 }
 @Test fun pauseOfflineAndDeleteThenRestart()=fixture{store,c,server->
  c.enroll(invite,MdmRights(vpn=true));server.offline=true
  val started=System.nanoTime();c.pause()
  assertTrue(System.nanoTime()-started<1_000_000_000);assertFalse(store.read().active)
  c.delete();assertNull(store.read().binding);assertEquals("",store.secret())
 }
 @Test fun redeemResponseLost()=fixture{store,c,server->
  server.loseOnce=true;assertTrue(runCatching{c.enroll(invite,MdmRights(config=true))}.isFailure)
  assertTrue(store.read().pendingEnrollment)
  assertTrue(c.enroll(invite,MdmRights(config=true)).active);assertEquals(1,server.enrollments)
 }
 @Test fun revokeRightInvalidatesInflightGeneration()=fixture{_,c,_->
  val s=c.enroll(invite,MdmRights(config=true,vpn=true))
  c.setRights(MdmRights(vpn=true))
  assertFalse(c.authorize(s.localGeneration,MdmRight.CONFIG))
 }
 @Test fun cleanupFailureIsRecoveredBeforeAuthorization()=fixture{store,c,server->
  val old=c.enroll(invite,MdmRights(config=true))
  val broken=MdmController(store,{server.gateway()},{error("interrupted cleanup")})
  assertTrue(runCatching{broken.pause()}.isFailure)
  assertTrue(store.read().cleanupPending);assertFalse(c.authorize(old.localGeneration,MdmRight.CONFIG))
  var clean=false
  MdmController(store,{server.gateway()},{clean=true}).recover()
  assertTrue(clean);assertFalse(store.read().cleanupPending)
 }
 @Test fun pauseClearsSavedRadioIntentEvenIfCleanupCrashes()=fixture{store,c,server->
  c.enroll(invite,MdmRights(telemetry=true,geo=true))
  store.edit{it.put("radio_manual",true)}
  val broken=MdmController(store,{server.gateway()},{error("interrupted cleanup")})
  assertTrue(runCatching{broken.pause()}.isFailure)
  assertFalse("Pause must durably clear collection intent",store.document().optBoolean("radio_manual"))
 }
 @Test fun revokingRadioConsentClearsSavedIntent()=fixture{store,c,_->
  c.enroll(invite,MdmRights(telemetry=true,geo=true))
  store.edit{it.put("radio_manual",true)}
  c.setRights(MdmRights(telemetry=true))
  c.setRights(MdmRights(telemetry=true,geo=true))
  assertFalse("Granting consent again must not restart old collection",store.document().optBoolean("radio_manual"))
 }

 @Test fun configRevocationCleanupSurvivesCrash()=fixture{store,c,server->
  c.enroll(invite,MdmRights(config=true,vpn=true))
  val broken=MdmController(store,{server.gateway()},{},{error("cleanup interrupted")})
  assertTrue(runCatching{broken.setRights(MdmRights(vpn=true))}.isFailure)
  assertTrue("revocation must journal cleanup",store.read().cleanupPending)
  var cleaned=false
  MdmController(store,{server.gateway()},{},{cleaned=true}).recover()
  assertTrue(cleaned);assertFalse(store.read().cleanupPending);assertFalse(store.read().rights.config)
 }

 @Test fun concurrentMainPauseDoesNotDeadlockCleanup()=fixture{store,c,server->
  c.enroll(invite,MdmRights(config=true,vpn=true))
  val gate=java.util.concurrent.CountDownLatch(1);val done=java.util.concurrent.CountDownLatch(1)
  val controller=MdmController(store,{server.gateway()},{MdmApplyCoordinator.onMain{}},{gate.countDown();MdmApplyCoordinator.onMain{}})
  val worker=Thread{runCatching{controller.setRights(MdmRights(vpn=true))}}
  android.os.Handler(android.os.Looper.getMainLooper()).post{
   gate.await(500,java.util.concurrent.TimeUnit.MILLISECONDS)
   try{controller.pause()}finally{done.countDown()}
  }
  worker.start()
  try{assertTrue("main/controller cleanup deadlock",done.await(3,java.util.concurrent.TimeUnit.SECONDS))}
  finally{worker.join(20000);done.await(20,java.util.concurrent.TimeUnit.SECONDS)}
 }

}
