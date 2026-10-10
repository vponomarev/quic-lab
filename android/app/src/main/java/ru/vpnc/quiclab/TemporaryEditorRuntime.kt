package ru.vpnc.quiclab

import android.content.Context
import org.json.JSONObject
import java.util.concurrent.atomic.AtomicBoolean

/** RAM-only user consent. No enrollment, no startup recovery and no VPN-start command. */
internal object TemporaryEditorRuntime {
 @Volatile var current:Session?=null;private set
 @Volatile var status="Редактор выключен";private set
 fun start(c:Context,invitation:TemporaryEditorInvitation){
  check(current==null){"Сначала завершите текущую сессию"}
  MdmConfiguration.assertEditable(c)
  val session=Session(c.applicationContext,invitation)
  current=session;status="Подключаем редактор…"
  try{c.startForegroundService(android.content.Intent(c,TemporaryEditorService::class.java))}
  catch(e:Exception){session.close();current=null;throw e}
 }
 fun stop(){current?.close();current=null;status="Доступ завершён"}
 class Session(private val c:Context,val invitation:TemporaryEditorInvitation,private val exchange:((String,JSONObject)->JSONObject)?=null,private val vpn:MdmVpnPort?=null){
  val lease=TemporaryEditorLease(invitation.minutes)
  private val closed=AtomicBoolean(false)
  private val managementGeneration=MdmConfiguration.token(c).first
  private var secret=invitation.token
  private var transport:MdmTransport?=null
  private var generation=0L
  private var last=""
  private var ack:JSONObject?=null
  private var paired=false
  private val requestLock=Any()
  fun valid()=!closed.get()&&lease.valid()
  fun checkAuthority(){check(valid());MdmConfiguration.assertEditable(c);check(MdmConfiguration.token(c).first==managementGeneration)}
  private fun report():String=synchronized(MdmConfiguration.lock){checkAuthority();MdmConfiguration.snapshotLocal(c).toString()}
  private fun request(op:String,body:JSONObject):JSONObject{
   checkAuthority()
   exchange?.let{return it(op,body)}
   val t=MdmTransport(c,{secret})
   synchronized(requestLock){check(valid());transport=t}
   try{return JSONObject(t.postEditor(invitation.endpoint,op,body.toString()))}
   finally{t.close();synchronized(requestLock){if(transport===t)transport=null}}
  }
  private fun inventory():JSONObject{
   val pm=c.packageManager;val entries=org.json.JSONArray()
   pm.getInstalledApplications(0).filter{it.packageName!=c.packageName&&it.packageName.matches(Regex("[A-Za-z][A-Za-z0-9_]*(\\.[A-Za-z0-9_]+)+"))}.sortedBy{it.packageName}.take(2048).forEach{
    entries.put(JSONObject().put("packageId",it.packageName).put("label",it.loadLabel(pm).toString().filter{ch->!Character.isISOControl(ch)}.take(256).ifEmpty{it.packageName}))
   }
   return JSONObject().put("complete",false).put("entries",entries)
  }
  private fun body()=JSONObject().put("id",invitation.id)
  fun sync(){
   checkAuthority()
   if(!paired){
    last=report()
    val grant=request("pair",body().put("generation",generation).put("configuration",JSONObject(last)).also{if(exchange==null)it.put("inventory",inventory())})
    secret=grant.getString("deviceToken");check(secret.matches(Regex("[0-9a-f]{64}")))
    paired=true;status="WEB-редактор подключён";return
   }
   ack?.let{request("ack",it);ack=null}
   val current=report()
   if(current!=last){generation++;request("report",body().put("generation",generation).put("configuration",JSONObject(current)));last=current}
   val state=request("poll",body());val pending=state.optJSONObject("pending")?:return
   val revision=pending.getLong("revision")
   var result="error"
   if(pending.getLong("baseGeneration")==generation)try{
    val raw=pending.getJSONObject("document").toString()
    mobile.Mobile.validateMDMConfiguration(raw)
    MdmApplyCoordinator.onMain {
     checkAuthority()
     synchronized(MdmConfiguration.lock){
      check(report()==last){"Локальные настройки изменились"}
      val before=ConfigurationTransfer.snapshot(c,true).toString();val doc=JSONObject(raw)
      // Preserve credentials only for the same explicit profile ID. New profiles must provide keys.
      val old=JSONObject(before).getJSONArray("profiles")
      val identities=(0 until old.length()).associate{old.getJSONObject(it).let{p->p.getString("id") to p.optJSONObject("identity")}}
      val profiles=doc.getJSONArray("profiles");val missing=mutableListOf<String>()
      for(i in 0 until profiles.length()){
       val p=profiles.getJSONObject(i)
       if(!p.has("identity")&&!p.optBoolean("removeIdentity"))identities[p.getString("id")]?.let{p.put("identity",it)}
       val transport=p.getJSONObject("settings").optString("transport","quic")
       val identity=p.optJSONObject("identity")
       val complete=when(transport){"vless"->identity?.has("vless_uri")==true;"awg"->identity?.has("awg_config")==true;else->identity?.has("key")==true&&identity.has("certificate")}
       if(!complete)missing.add(p.getString("id"))
      }
            val enabled=doc.getJSONArray("enabledProfileIds")
      doc.put("enabledProfileIds",org.json.JSONArray((0 until enabled.length()).map{enabled.getString(it)}.filter{it !in missing}))
      val preview=TransferPreview(doc.toString(),before,MdmConfiguration.token(c),emptyList(),missing,"WEB-редактор")
      if(vpn==null)LocalConfigurationApply.apply(c,preview,guard={checkAuthority()}) else LocalConfigurationApply.apply(c,preview,vpn,guard={checkAuthority()})
     }
    }
    result="applied"
   }catch(_:Exception){status="Не удалось применить настройки. Проверьте конфиг, права и приложения."}
   checkAuthority();last=report();generation++
   ack=body().put("revision",revision).put("generation",generation).put("configuration",JSONObject(last)).put("status",result)
   request("ack",ack!!);ack=null
   if(result=="applied")status="Настройки применены"
  }
  fun close(){
   if(!closed.compareAndSet(false,true))return
   lease.revoke();synchronized(requestLock){transport?.close()}
   // Best effort notification is separate from local revocation, which has already completed.
   if(paired && exchange==null){val credential=secret;Thread{runCatching{MdmTransport(c,{credential}).use{it.postEditor(invitation.endpoint,"revoke",body().toString())}}}.start()}
   secret=""
  }
 }
}
