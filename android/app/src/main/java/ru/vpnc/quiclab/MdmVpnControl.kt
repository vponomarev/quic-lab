package ru.vpnc.quiclab

import android.content.Context
import android.content.Intent
import android.net.VpnService
import org.json.JSONArray
import org.json.JSONObject
import java.time.Instant
import java.util.UUID

internal object MdmVpnControl {
 fun validate(s:MdmState,c:MdmCommand,now:Instant):String {
  if(!s.active || s.cleanupPending || !s.rights.vpn)return "permission_denied"
  if(c.bindingId!=s.binding?.id || c.epoch!=s.binding.epoch)return "stale_epoch"
  if(!now.isBefore(c.expiresAt) || c.issuedAt.isAfter(now.plusSeconds(30)))return "expired"
  return if(c.kind in listOf("vpn_start","vpn_stop"))"" else "unsupported"
 }
 fun event(context:Context,kind:String,result:String="",revision:Long=0,commandID:String="",actor:String="device"){
  val store=MdmStore(context)
  store.edit{j->
   if(!j.optBoolean("active"))return@edit
   if(kind=="config_result" || kind=="config_vpn_result"){
    val signature="${j.optJSONObject("binding")?.optLong("epoch")}:$revision:$result"
    val key="last_$kind"
    if(j.optString(key)==signature)return@edit
    j.put(key,signature)
   }
   val events=j.optJSONArray("events")?:JSONArray()
   if(events.length()>=1000)events.remove(0)
   events.put(JSONObject().put("id",UUID.randomUUID().toString()).put("actor",actor).put("kind",kind).put("result",result)
    .put("revision",revision).put("commandId",commandID).put("occurredAt",Instant.now().toString()))
   j.put("events",events)
  }
 }
 fun execute(context:Context,state:MdmState,command:MdmCommand,serverTime:Instant,receivedElapsed:Long=android.os.SystemClock.elapsedRealtime(),action:(()->String)?=null):String=MdmApplyCoordinator.onMain {
  val now=serverTime.plusMillis((android.os.SystemClock.elapsedRealtime()-receivedElapsed).coerceAtLeast(0))
  synchronized(MdmConfiguration.lock){
   val store=MdmStore(context);val latest=store.read()
   var error=validate(latest,command,now)
   if(latest.localGeneration!=state.localGeneration)error="permission_denied"
   if(error.isNotEmpty()){event(context,"command_result",error,commandID=command.id);return@synchronized error}
   val prior=store.document().optJSONObject("commands_done")?.optJSONObject(command.id)
   if(prior!=null){val result=prior.optString("result","interrupted");event(context,"command_result",result,commandID=command.id);return@synchronized result}
   store.edit{j->
    val done=j.optJSONObject("commands_done")?:JSONObject()
    done.keys().asSequence().toList().forEach{if(done.getJSONObject(it).optLong("until")<now.epochSecond)done.remove(it)}
    check(done.length()<1000)
    done.put(command.id,JSONObject().put("result","interrupted").put("until",command.expiresAt.plusSeconds(86400).epochSecond));j.put("commands_done",done)
   }
   val result=try{
    if(action!=null)action()
    else if(command.kind=="vpn_stop"){
     store.edit{it.put("user_stop",Math.addExact(it.optLong("user_stop"),1))}
     LabVpnService.stopForMdm(true);"stopped"
    }else if(VpnService.prepare(context)!=null)"needs_android_consent"
    else if(LabVpnService.active)"running"
    else {context.startForegroundService(Intent(context,LabVpnService::class.java).putExtra("mdm_restart",true)
     .putExtra("mdm_user_stop",store.document().optLong("user_stop")).putExtra("mdm_required_generation",state.localGeneration)
     .putExtra("mdm_right","vpn").putExtra("mdm_command_id",command.id));"starting"}
   }catch(_:Exception){"vpn_action_failed"}
   store.edit{it.getJSONObject("commands_done").getJSONObject(command.id).put("result",result)}
   event(context,"command_result",result,commandID=command.id);result
  }
 }
}
