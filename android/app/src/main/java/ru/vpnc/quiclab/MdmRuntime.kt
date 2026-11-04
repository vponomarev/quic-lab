package ru.vpnc.quiclab

import android.content.Context
import org.json.JSONObject
import java.util.concurrent.atomic.AtomicBoolean

/** Composition for voluntary device management; has no dependency on a VPN session. */
internal object MdmRuntime {
 private var instance:MdmController?=null
 private val sessions=MdmSessionGate()
 fun controller(context:Context):MdmController {
  val controller=synchronized(this){instance?:createController(context).also{instance=it}}
  controller.recover()
  return controller
 }
 private fun createController(context:Context):MdmController {
  val app=context.applicationContext
  return MdmController(MdmStore(app),{object:MdmGateway{
   private val closed=AtomicBoolean(false)
   private var transfer:MdmTransport?=null
   override fun post(b:MdmBinding,op:String,body:JSONObject,secret:String):JSONObject{
    val t=MdmTransport(app,{secret})
    synchronized(this){check(!closed.get());transfer=t}
    return try{JSONObject(t.post(b,op,body.toString()))}finally{t.close()}
   }
   override fun close(){synchronized(this){closed.set(true);transfer?.close()}}
  }},{
   MdmTelemetry.stop(app,true)
   MdmApplyCoordinator(app).detach()
  },{MdmTelemetry.rightsChanged(app);if(!MdmStore(app).read().rights.config)MdmApplyCoordinator(app).detach()},stateChanged={
   ResearchSettings.changed()
   android.os.Handler(android.os.Looper.getMainLooper()).post{restore(app)}
  })
 }
 fun restore(context:Context,foreground:Boolean=false){
  runCatching {
   val state=controller(context).read()
   MdmBackground.reconcile(context,foreground)
   if(state.active && !state.cleanupPending && state.binding!=null){
    MdmTelemetry.reconcile(context,foreground)
   }
  }.onFailure {
   MdmService.status="Не удалось восстановить MDM: проверьте автозапуск и откройте приложение"
   // Exception class is sufficient here; never log credentials or server responses.
   Diagnostics.event("mdm",JSONObject().put("event","restore_denied").put("error",it.javaClass.simpleName))
  }
 }
 fun session(context:Context,quiet:Boolean=false,selectedController:MdmController=controller(context)):MdmSession?=sessions.open{
  val c=selectedController
  MdmApplyCoordinator(context).recover()
  if(!c.read().active)return@open null
  object:MdmSession{
   private val closed=AtomicBoolean(false)
   private fun continuing()=!closed.get() && !Thread.currentThread().isInterrupted
   override fun isActive()=continuing() && c.read().active
   private fun checkActive(){if(!isActive() || Thread.currentThread().isInterrupted)throw InterruptedException("MDM session closed")}
   override fun sync(){
    checkActive()
    var delivered:Pair<MdmState,MdmSyncResponse>?=null
    var received=android.os.SystemClock.elapsedRealtime()
    if(!c.poll(wait=!quiet && !VlessProbeCoordinator.busy(),allowed=::continuing){state,response->
     checkActive()
     MdmTelemetry.setServerEnabled(context,response.telemetryEnabled)
     delivered=state to response;received=android.os.SystemClock.elapsedRealtime()
    })
     throw InterruptedException("MDM paused")
    val (state,response)=delivered?:throw InterruptedException("No MDM response")
    checkActive()
    val desired=response.desiredConfig
    var applyFailed=false
    if(desired!=null){val result=MdmApplyCoordinator(context).apply(state,desired);applyFailed=!result.configurationApplied;MdmVpnControl.event(context,"config_result",if(result.configurationApplied)"applied" else result.errorCode,result.revision);if(result.configurationApplied && result.errorCode.isNotEmpty())MdmVpnControl.event(context,"config_vpn_result",result.errorCode,result.revision)}
    for(command in response.commands){
     checkActive()
     if(command.kind=="vless_probe")MdmProbeControl.execute(context,state,command,response.serverTime,allowStart=!quiet)
     else MdmVpnControl.execute(context,state,command,response.serverTime,received)
    }
    checkActive()
    runCatching{c.report(allowed=::continuing){current->MdmDeviceReport.snapshot(context,current)}}
    checkActive()
    if(quiet)MdmTelemetry.sampleOnce(context) else MdmTelemetry.reconcile(context)
    checkActive()
    runCatching{MdmTelemetry.upload(context,allowed=::continuing)}
    checkActive()
    if(quiet){
     // Send applied revision/events without waiting or recursively applying another revision.
     c.poll(wait=false,allowed=::continuing){_,_->checkActive()}
     MdmService.status="MDM · тихая синхронизация завершена"
    }else if(desired!=null || response.commands.isNotEmpty() || VlessProbeCoordinator.busy())Thread.sleep(if(applyFailed)15000 else 1000)
   }
   override fun pause(){c.pause()}
   override fun close(){closed.set(true);c.cancelCalls();if(quiet)MdmTelemetry.cancelUpload()}
  }
 }
}
