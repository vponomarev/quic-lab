package ru.vpnc.quiclab

import android.content.Context
import android.content.Intent
import android.net.VpnService
import android.os.Handler
import android.os.Looper
import org.json.JSONObject
import java.util.concurrent.FutureTask
import java.util.concurrent.TimeUnit

internal interface MdmVpnPort {fun active():Boolean;fun stop();fun start():String}
internal data class MdmApplyResult(val revision:Long,val configurationApplied:Boolean,val vpnState:String,val errorCode:String="")
internal class MdmApplyCoordinator(private val context:Context,private val vpn:MdmVpnPort=AndroidPort(context),private val checkpoint:(String)->Unit={}) {
 private val store=MdmStore(context)
 private fun allowed(state:MdmState)=store.read().let{it.active && !it.cleanupPending && it.rights.config && it.localGeneration==state.localGeneration && it.binding?.id==state.binding?.id}
 fun apply(state:MdmState,desired:MdmConfigRevision):MdmApplyResult=onMain {
  synchronized(MdmConfiguration.lock){
   if(!allowed(state))return@synchronized MdmApplyResult(desired.revision,false,if(vpn.active())"running" else "stopped","permission_denied")
   val config=MdmConfigurationStore(context);val binding=state.binding!!.id
   if(config.revision(binding)>=desired.revision){ack(desired.revision);return@synchronized MdmApplyResult(desired.revision,true,if(vpn.active())"running" else "stopped")}
   try{
    mobile.Mobile.validateMDMConfiguration(desired.document.toString())
    val generation=MdmDeviceReport.configurationGeneration(context)
    check(desired.expectedGeneration<0 || desired.expectedGeneration==generation || store.document().optBoolean("resume_config")){"generation_conflict"}
    val operation=JSONObject().put("binding",binding).put("revision",desired.revision).put("phase","prepared")
     .put("wasRunning",vpn.active()).put("userStop",store.document().optLong("user_stop"))
    store.edit{it.put("apply_operation",operation)};checkpoint("prepared")
    if(vpn.active())vpn.stop()
    check(allowed(state)){"permission_denied"}
    MdmConfiguration.apply(context,desired,state.localGeneration);checkpoint("committed")
    store.edit{it.getJSONObject("apply_operation").put("phase","committed")};ack(desired.revision)
    val next=restart(operation);store.edit{it.remove("apply_operation")};checkpoint("completed")
    MdmApplyResult(desired.revision,true,next,if(next=="error")"vpn_start_failed" else "")
   }catch(e:Exception){
    val applied=config.revision(binding)>=desired.revision
    if(applied)ack(desired.revision)
    val op=store.document().optJSONObject("apply_operation")
    if(op!=null && allowed(state))runCatching{restart(op)}
    store.edit{it.remove("apply_operation")}
    MdmApplyResult(desired.revision,applied,if(vpn.active())"running" else "stopped",if(e.message=="generation_conflict")"generation_conflict" else "apply_failed")
   }
  }
 }
 private fun ack(revision:Long){store.edit{it.put("appliedRevision",revision);it.remove("resume_config")}}
 private fun restart(op:JSONObject):String {
  if(op.optBoolean("wasRunning") && store.document().optLong("user_stop")==op.optLong("userStop"))return runCatching{vpn.start()}.getOrDefault("error")
  return if(vpn.active())"running" else "stopped"
 }
 fun recover()=onMain {
  synchronized(MdmConfiguration.lock){
   val op=store.document().optJSONObject("apply_operation")?:return@synchronized
   val state=store.read()
   if(!state.active || state.cleanupPending){detach();return@synchronized}
   val revision=MdmConfigurationStore(context).revision(op.optString("binding"))
   if(revision>=op.optLong("revision"))ack(revision)
   restart(op);store.edit{it.remove("apply_operation")}
  }
 }
 fun detach()=onMain {
  synchronized(MdmConfiguration.lock){
   val external=MdmConfigurationStore(context).mode()=="external"
   val running=vpn.active()
   if(external && running)vpn.stop()
   MdmConfiguration.detach(context)
   store.edit{it.remove("apply_operation");it.put("appliedRevision",0)}
   if(external && running)vpn.start()
  }
 }
 companion object {
  fun <T> onMain(block:()->T):T {
   if(Looper.myLooper()==Looper.getMainLooper())return block()
   val work=FutureTask<T>{block()};Handler(Looper.getMainLooper()).post(work)
   return try{work.get(15,TimeUnit.SECONDS)}catch(e:Exception){work.cancel(false);throw e}
  }
 }
 private class AndroidPort(private val c:Context):MdmVpnPort {
  override fun active()=LabVpnService.active
  override fun stop(){LabVpnService.stopForMdm()}
  override fun start():String {
   if(VpnService.prepare(c)!=null)return "error"
   val state=MdmStore(c).read()
   c.startForegroundService(Intent(c,LabVpnService::class.java).putExtra("mdm_required_generation",if(state.active && state.rights.config)state.localGeneration else -1L).putExtra("mdm_restart",true).putExtra("mdm_revision",MdmStore(c).read().appliedRevision).putExtra("mdm_user_stop",MdmStore(c).document().optLong("user_stop")))
   return "starting"
  }
 }
}
