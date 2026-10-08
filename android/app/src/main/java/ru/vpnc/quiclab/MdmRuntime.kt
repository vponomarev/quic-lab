package ru.vpnc.quiclab

import android.content.Context
import org.json.JSONObject
import java.util.concurrent.atomic.AtomicBoolean

/** Composition for voluntary device management; has no dependency on a VPN session. */
internal object MdmRuntime {
 private var instance:MdmController?=null
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
  },{MdmTelemetry.rightsChanged(app);if(!MdmStore(app).read().rights.config)MdmApplyCoordinator(app).detach()})
 }
 fun restore(context:Context,foreground:Boolean=false){
  runCatching{if(controller(context).read().active){MdmService.start(context);MdmTelemetry.reconcile(context,foreground)}}
   .onFailure{MdmService.status="Не удалось восстановить MDM; откройте настройки управления"}
 }
 fun session(context:Context):MdmSession?{
  val c=controller(context)
  MdmApplyCoordinator(context).recover()
  if(!c.read().active)return null
  return object:MdmSession{
   private val closed=AtomicBoolean(false)
   override fun isActive()=!closed.get() && c.read().active
   override fun sync(){
    var delivered:Pair<MdmState,MdmSyncResponse>?=null
    var received=android.os.SystemClock.elapsedRealtime()
    if(!c.poll{state,response->
     MdmTelemetry.setServerEnabled(context,response.telemetryEnabled)
     delivered=state to response;received=android.os.SystemClock.elapsedRealtime()
    })
     throw InterruptedException("MDM paused")
    val (state,response)=delivered?:throw InterruptedException("No MDM response")
    val desired=response.desiredConfig
    var applyFailed=false
    if(desired!=null){val result=MdmApplyCoordinator(context).apply(state,desired);applyFailed=!result.configurationApplied;MdmVpnControl.event(context,"config_result",if(result.configurationApplied)"applied" else result.errorCode,result.revision);if(result.configurationApplied && result.errorCode.isNotEmpty())MdmVpnControl.event(context,"config_vpn_result",result.errorCode,result.revision)}
    for(command in response.commands)MdmVpnControl.execute(context,state,command,response.serverTime,received)
    runCatching{c.report{current->MdmDeviceReport.snapshot(context,current)}}
    MdmTelemetry.reconcile(context)
    runCatching{MdmTelemetry.upload(context)}
    if(desired!=null || response.commands.isNotEmpty())Thread.sleep(if(applyFailed)15000 else 1000)
   }
   override fun pause(){c.pause()}
   override fun close(){closed.set(true);c.cancelCalls()}
  }
 }
}
