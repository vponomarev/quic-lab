package ru.vpnc.quiclab

import android.util.Base64
import org.json.JSONObject
import java.io.Closeable
import java.net.URI
import java.security.SecureRandom
import java.util.UUID
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

internal enum class MdmRight { CONFIG,VPN,TELEMETRY,GEO,COORDINATES,LAN }
internal data class MdmInvitation(val endpoint:String,val token:String,val rights:MdmRights,val mode:String="current"){
 init{
  MdmBinding("",endpoint,0,rights,false)
  require(token.matches(Regex("[0-9a-f]{64}")))
  require(mode=="current"||mode=="external")
 }
 companion object{
  fun parse(link:String):MdmInvitation{
   require(link.length<=16384)
   val u=URI(link)
   require(u.scheme=="quiclab" && u.host=="mdm" && u.path=="/enroll" &&
    u.rawUserInfo==null && u.port==-1 && u.rawQuery==null && !u.rawFragment.isNullOrEmpty())
   val raw=Base64.decode(u.rawFragment,Base64.URL_SAFE or Base64.NO_WRAP or Base64.NO_PADDING)
   val j=JSONObject(String(raw,Charsets.UTF_8));require(j.getInt("version")==1)
   return MdmInvitation(j.getString("endpoint"),j.getString("token"),
    MdmRights.parse(j.getJSONObject("requestedRights")),j.optString("mode","current"))
  }
 }
}
internal interface MdmGateway:Closeable {
 fun post(b:MdmBinding,op:String,body:JSONObject,secret:String):JSONObject
}

/** No network call holds the storage lock. Durable generations reject every late result. */
internal class MdmController(
 private val store:MdmStore,
 private val gateways:()->MdmGateway,
 private val cleanup:()->Unit,
) {
 private val calls=ConcurrentHashMap<MdmGateway,Boolean>()
 fun read()=store.read()
 @Synchronized fun recover(){
  if(!store.read().cleanupPending)return
  cleanup()
  store.edit{j->
   if(j.optBoolean("deletePending")){
    val generation=Math.addExact(j.getLong("generation"),1)
    j.keys().asSequence().toList().forEach{j.remove(it)}
    j.put("schema",1).put("generation",generation)
   }else j.put("cleanupPending",false)
  }
 }
 fun enroll(invitation:MdmInvitation,rights:MdmRights):MdmState {
  recover()
  val state=store.edit{j->
   check(!j.has("binding")){"Сначала удалите существующую привязку MDM"}
   val pending=j.optJSONObject("pending")
   if(pending!=null){
    check(pending.getString("endpoint")==invitation.endpoint && pending.getString("token")==invitation.token){"Сначала удалите незавершённую регистрацию"}
   }else{
    j.put("secret",newSecret()).put("pending",JSONObject().put("endpoint",invitation.endpoint)
     .put("token",invitation.token).put("registrationId",UUID.randomUUID().toString()))
   }
   j.put("rights",rights.json()).put("active",false)
    .put("generation",Math.addExact(j.getLong("generation"),1))
  }
  val saved=store.document();val pending=saved.getJSONObject("pending")
  val provisional=MdmBinding("",invitation.endpoint,0,invitation.rights,false)
  val req=JSONObject().put("version",1).put("token",pending.getString("token"))
   .put("registrationId",pending.getString("registrationId")).put("secret",saved.getString("secret"))
  val response=send(provisional,"enroll",req,state.localGeneration,saved.getString("secret"))
  val b=binding(response,invitation.endpoint)
  store.edit{j->
   check(j.getLong("generation")==state.localGeneration && j.has("pending")){"Регистрация отменена"}
   j.put("binding",MdmStore.bindingJson(b));j.remove("pending")
  }
  return resume()
 }
 fun resume():MdmState {
  recover()
  val state=store.edit{j->
   check(j.has("binding")){"Нет привязки MDM"}
   j.put("active",false).put("generation",Math.addExact(j.getLong("generation"),1))
  }
  cancelCalls()
  val b=state.binding!!
  val reply=send(b,"activate",MdmSyncRequest(b.id,b.epoch,state.appliedRevision,wait=false).json(),
   state.localGeneration,store.secret())
  val activated=binding(reply,b.endpoint)
  check(activated.id==b.id && activated.active && activated.epoch>b.epoch){"Неверная эпоха MDM"}
  return store.edit{j->
   check(j.getLong("generation")==state.localGeneration && j.has("binding") && !j.optBoolean("cleanupPending")){"Возобновление отменено"}
   j.put("binding",MdmStore.bindingJson(activated)).put("active",true)
  }
 }
 @Synchronized fun pause(){
  val before=store.read();val secret=store.secret()
  val paused=store.edit{j->
   j.put("active",false).put("cleanupPending",true).put("generation",Math.addExact(j.getLong("generation"),1))
  }
  cancelCalls()
  recover()
  before.binding?.let{b->notifyPause(b,secret,paused.localGeneration)}
 }
 @Synchronized fun delete(){
  store.edit{j->j.put("active",false).put("cleanupPending",true).put("deletePending",true)
   .put("generation",Math.addExact(j.getLong("generation"),1))}
  cancelCalls();recover()
 }
 @Synchronized fun setRights(rights:MdmRights){
  store.edit{j->
   check(j.has("binding")){"Нет привязки MDM"}
   j.put("rights",rights.json()).put("generation",Math.addExact(j.getLong("generation"),1))
  }
  cancelCalls()
 }
 fun authorize(generation:Long,right:MdmRight):Boolean {
  val s=store.read()
  if(!s.active || s.cleanupPending || s.localGeneration!=generation)return false
  return when(right){
   MdmRight.CONFIG->s.rights.config;MdmRight.VPN->s.rights.vpn;MdmRight.TELEMETRY->s.rights.telemetry
   MdmRight.GEO->s.rights.geo;MdmRight.COORDINATES->s.rights.coordinates;MdmRight.LAN->s.rights.lanMode!="deny"
  }
 }
 internal fun cancelCalls(){calls.keys.forEach{runCatching{it.close()}}}
 private fun send(b:MdmBinding,op:String,body:JSONObject,generation:Long,secret:String):JSONObject{
  val gateway=gateways();calls[gateway]=true
  try{
   check(store.read().localGeneration==generation){"Операция MDM отменена"}
   return gateway.post(b,op,body,secret)
  }finally{calls.remove(gateway);gateway.close()}
 }
 private fun notifyPause(b:MdmBinding,secret:String,generation:Long){
  Thread({
   val deadline=System.nanoTime()+TimeUnit.SECONDS.toNanos(60)
   repeat(3){
    if(System.nanoTime()>=deadline || runCatching{store.read().localGeneration}.getOrNull()!=generation)return@Thread
    val gateway=gateways()
    val timer=scheduler.schedule({gateway.close()},(deadline-System.nanoTime()).coerceAtLeast(0),TimeUnit.NANOSECONDS)
    try{
     gateway.post(b,"pause",MdmSyncRequest(b.id,b.epoch,wait=false).json(),secret)
     return@Thread
    }catch(_:Exception){}finally{timer.cancel(false);gateway.close()}
   }
  },"mdm-pause-notify").apply{isDaemon=true;start()}
 }
 companion object{
  private val scheduler=Executors.newSingleThreadScheduledExecutor{r->Thread(r,"mdm-deadline").apply{isDaemon=true}}
  private fun newSecret()=ByteArray(32).also{SecureRandom().nextBytes(it)}.joinToString(""){"%02x".format(it)}
  private fun binding(j:JSONObject,endpoint:String)=MdmBinding(j.getString("id"),endpoint,j.getLong("epoch"),
   MdmRights.parse(j.getJSONObject("requestedRights")),j.getBoolean("active"))
 }
}
