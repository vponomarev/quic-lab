package ru.vpnc.quiclab

import android.content.Context
import org.json.JSONObject
import java.util.concurrent.atomic.AtomicBoolean

/** Composition for voluntary device management; has no dependency on a VPN session. */
internal object MdmRuntime {
 private var instance:MdmController?=null
 @Synchronized fun controller(context:Context):MdmController {
  instance?.let{return it}
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
   // No configuration layer or remote actions are enabled before Tasks 4/5.
  }).also{it.recover();instance=it}
 }
 fun restore(context:Context){
  runCatching{if(controller(context).read().active)MdmService.start(context)}
   .onFailure{MdmService.status="Не удалось восстановить MDM; откройте настройки управления"}
 }
 fun session(context:Context):MdmSession?{
  val c=controller(context)
  if(!c.read().active)return null
  return object:MdmSession{
   private val closed=AtomicBoolean(false)
   override fun isActive()=!closed.get() && c.read().active
   override fun sync(){
    var pending=false
    if(!c.poll{_,response->pending=response.desiredConfig!=null || response.commands.isNotEmpty()})
     throw InterruptedException("MDM paused")
    // Do not acknowledge unsupported actions. Avoid a tight loop until their consumers are wired.
    if(pending)Thread.sleep(1000)
   }
   override fun pause(){c.pause()}
   override fun close(){closed.set(true);c.cancelCalls()}
  }
 }
}
