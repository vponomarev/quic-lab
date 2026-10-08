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
 private val rightsChanged:()->Unit={},
) {
 private val calls=ConcurrentHashMap<MdmGateway,Boolean>()
 fun read()=store.read()
 private fun mutate(change:(JSONObject)->Unit):MdmState=synchronized(this){synchronized(MdmConfiguration.lock){store.edit(change)}}
 fun recover(){ MdmApplyCoordinator.onMain { synchronized(this) {
  if(!store.read().cleanupPending)return@synchronized
  if(store.document().optBoolean("configCleanupPending") && !store.document().optBoolean("cleanupPending"))rightsChanged() else cleanup()
  mutate{j->
   if(j.optBoolean("deletePending")){
    val generation=Math.addExact(j.getLong("generation"),1)
    j.keys().asSequence().toList().forEach{j.remove(it)}
    j.put("schema",1).put("generation",generation)
   }else {j.put("cleanupPending",false);j.remove("configCleanupPending")}
  }
 } } }
 fun enroll(invitation:MdmInvitation,rights:MdmRights):MdmState {
  recover()
  val state=mutate{j->
   check(!j.has("binding")){"Сначала удалите существующую привязку MDM"}
   val pending=j.optJSONObject("pending")
   if(pending!=null){
    check(pending.getString("endpoint")==invitation.endpoint && pending.getString("token")==invitation.token){"Сначала удалите незавершённую регистрацию"}
   }else{
    j.put("secret",newSecret()).put("pending",JSONObject().put("endpoint",invitation.endpoint)
     .put("token",invitation.token).put("registrationId",UUID.randomUUID().toString()))
   }
   j.put("rights",rights.json()).put("radio_manual",false).put("active",false)
    .put("generation",Math.addExact(j.getLong("generation"),1))
  }
  val saved=store.document();val pending=saved.getJSONObject("pending")
  val provisional=MdmBinding("",invitation.endpoint,0,invitation.rights,false)
  val req=JSONObject().put("version",1).put("token",pending.getString("token"))
   .put("registrationId",pending.getString("registrationId")).put("secret",saved.getString("secret"))
  val response=send(provisional,"enroll",req,state.localGeneration,saved.getString("secret"))
  val b=binding(response,invitation.endpoint)
  mutate{j->
   check(j.getLong("generation")==state.localGeneration && j.has("pending")){"Регистрация отменена"}
   j.put("binding",MdmStore.bindingJson(b));j.remove("pending")
  }
  return resume()
 }
 fun resume():MdmState {
  recover()
  val state=mutate{j->
   check(j.has("binding")){"Нет привязки MDM"}
   j.put("active",false).put("generation",Math.addExact(j.getLong("generation"),1))
  }
  cancelCalls()
  val b=state.binding!!
  val reply=send(b,"activate",MdmSyncRequest(b.id,b.epoch,state.appliedRevision,wait=false).json(),
   state.localGeneration,store.secret())
  val activated=binding(reply,b.endpoint)
  check(activated.id==b.id && activated.active && activated.epoch>b.epoch){"Неверная эпоха MDM"}
  return mutate{j->
   check(j.getLong("generation")==state.localGeneration && j.has("binding") && !j.optBoolean("cleanupPending")){"Возобновление отменено"}
   j.put("binding",MdmStore.bindingJson(activated)).put("active",true).put("resume_config",true)
  }
 }
 fun pause(){ MdmApplyCoordinator.onMain { synchronized(this) {
  val before=store.read();val secret=store.secret()
  val paused=mutate{j->
   j.put("active",false).put("radio_manual",false).put("cleanupPending",true).put("generation",Math.addExact(j.getLong("generation"),1))
  }
  cancelCalls()
  recover()
  before.binding?.let{b->notifyPause(b,secret,paused.localGeneration)}
 } } }
 fun delete(){ MdmApplyCoordinator.onMain { synchronized(this) {
  mutate{j->j.put("active",false).put("radio_manual",false).put("cleanupPending",true).put("deletePending",true)
   .put("generation",Math.addExact(j.getLong("generation"),1))}
  cancelCalls();recover()
 } } }
 fun setRights(rights:MdmRights){ MdmApplyCoordinator.onMain { synchronized(this) {
  mutate{j->
   check(j.has("binding")){"Нет привязки MDM"}
   if(j.optJSONObject("rights")?.optBoolean("config")==true && !rights.config)j.put("configCleanupPending",true)
   j.put("rights",rights.json()).put("generation",Math.addExact(j.getLong("generation"),1))
   if(!rights.geo || !rights.telemetry)j.put("radio_manual",false)
  }
  cancelCalls();if(store.read().cleanupPending)recover() else rightsChanged()
 } } }
 fun authorize(generation:Long,right:MdmRight):Boolean {
  val s=store.read()
  if(!s.active || s.cleanupPending || s.localGeneration!=generation)return false
  return when(right){
   MdmRight.CONFIG->s.rights.config;MdmRight.VPN->s.rights.vpn;MdmRight.TELEMETRY->s.rights.telemetry
   MdmRight.GEO->s.rights.geo;MdmRight.COORDINATES->s.rights.coordinates;MdmRight.LAN->s.rights.lanMode!="deny"
  }
 }
 internal fun poll(consume:(MdmState,MdmSyncResponse)->Unit):Boolean{
  val state=store.read()
  if(!state.active || state.cleanupPending)return false
  val b=state.binding?:return false
  val events=store.document().optJSONArray("events")?:org.json.JSONArray()
  val sent=org.json.JSONArray();for(i in 0 until minOf(100,events.length()))sent.put(events.getJSONObject(i))
  val response=MdmSyncResponse.parse(send(b,"sync",
   MdmSyncRequest(b.id,b.epoch,state.appliedRevision,events=sent,grantedRights=state.rights).json(),state.localGeneration,store.secret()).toString())
  synchronized(this){
   val latest=store.read()
   if(!latest.active || latest.cleanupPending || latest.localGeneration!=state.localGeneration)return false
   check(response.epoch==b.epoch){"Эпоха ответа MDM не совпадает"}
   synchronized(MdmConfiguration.lock){
    val permitted=store.read()
    if(!permitted.active || permitted.cleanupPending || permitted.localGeneration!=state.localGeneration)return false
    val ids=(0 until sent.length()).map{sent.getJSONObject(it).getString("id")}.toSet()
    store.edit{j->val pending=j.optJSONArray("events")?:org.json.JSONArray();val keep=org.json.JSONArray();for(i in 0 until pending.length())if(pending.getJSONObject(i).optString("id") !in ids)keep.put(pending.getJSONObject(i));j.put("events",keep)}
    consume(permitted,response)
   }
  }
  return true
 }
 internal fun report(build:(MdmState)->JSONObject):Boolean {
  val before=read();if(!before.active || before.cleanupPending)return false
  val b=before.binding?:return false
  val report=build(before)
  val latest=read();if(!latest.active || latest.localGeneration!=before.localGeneration)return false
  try{send(b,"report",JSONObject().put("bindingId",b.id).put("epoch",b.epoch).put("report",report),before.localGeneration,store.secret())}
  catch(e:Exception){store.edit{it.remove("inventory_ack")};throw e}
  synchronized(this){if(read().localGeneration==before.localGeneration)store.edit{j->
   val hash=report.optJSONObject("inventory")?.optString("hash")
   if(hash!=null)j.put("inventory_ack",hash)else j.remove("inventory_ack")
  }}
  return true
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
