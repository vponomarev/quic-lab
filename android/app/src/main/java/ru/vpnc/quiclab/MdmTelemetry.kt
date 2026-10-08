package ru.vpnc.quiclab

import android.content.Context
import org.json.JSONObject

internal object MdmTelemetryPolicy{
 fun allows(wifiOnly:Boolean,wifi:Boolean,metered:Boolean)=!wifiOnly || wifi
 fun consented(s:MdmState)=s.active && !s.cleanupPending && s.binding!=null && s.rights.telemetry && s.rights.geo
}
internal object MdmTelemetry {
 internal val lock=Any()
 @Volatile var manual=false
 @Volatile var status="Сбор выключен"
 private val uploading=java.util.concurrent.atomic.AtomicBoolean(false)
 @Volatile private var transfer:MdmTransport?=null
 private fun prefs(c:Context)=c.getSharedPreferences("mdm-radio-options",Context.MODE_PRIVATE)
 fun wifiOnly(c:Context)=prefs(c).getBoolean("wifi_only",false)
 fun serverEnabled(c:Context)=prefs(c).getString("server_binding","")==MdmStore(c).read().binding?.id && prefs(c).getBoolean("server_enabled",false)
 fun setWifiOnly(c:Context,value:Boolean)=synchronized(lock){
  check(prefs(c).edit().putBoolean("wifi_only",value).commit())
  transfer?.close()
 }
 fun setServerEnabled(c:Context,value:Boolean){
  prefs(c).edit().putString("server_binding",MdmStore(c).read().binding?.id).putBoolean("server_enabled",value).apply()
 }

 fun reconcile(c:Context){
  if(!wanted(c)){if(MdmTelemetryService.running)MdmTelemetryService.stop(c);return}
  if(MdmTelemetryService.running)return
  val allowed=c.checkSelfPermission(android.Manifest.permission.ACCESS_FINE_LOCATION)==android.content.pm.PackageManager.PERMISSION_GRANTED &&
   c.checkSelfPermission(android.Manifest.permission.ACCESS_BACKGROUND_LOCATION)==android.content.pm.PackageManager.PERMISSION_GRANTED
  if(!allowed){status="Сервер запросил сбор · откройте экран телеметрии или разрешите геопозицию всегда";return}
  runCatching{MdmTelemetryService.start(c)}
   .onFailure{status="Android запретил фоновый запуск · откройте экран телеметрии"}
 }
 fun wanted(c:Context)=MdmTelemetryPolicy.consented(MdmStore(c).read())&&(manual||serverEnabled(c))
 fun stop(c:Context,erase:Boolean){
  synchronized(lock){
   manual=false;transfer?.close();transfer=null
   prefs(c).edit().remove("server_binding").remove("server_enabled").remove("last_upload").remove("reported_dropped").apply()
   if(erase)MdmTelemetryStore(c).erase()
   status="Сбор выключен"
  }
  MdmTelemetryService.stop(c)
 }
 fun rightsChanged(c:Context){
  if(!MdmTelemetryPolicy.consented(MdmStore(c).read()))stop(c,true)
  else synchronized(lock){transfer?.close()}
 }
 fun append(c:Context,generation:Long,sample:JSONObject)=synchronized(lock){
  val state=MdmStore(c).read()
  if(state.localGeneration==generation && wanted(c))MdmTelemetryStore(c).append(sample,System.currentTimeMillis())
 }
 fun upload(c:Context){
  if(!uploading.compareAndSet(false,true))return
  try{uploadOnce(c)}finally{uploading.set(false)}
 }
 private fun uploadOnce(c:Context){
  val store=MdmStore(c)
  val state=store.read()
  if(!MdmTelemetryPolicy.consented(state))return
  val queue=MdmTelemetryStore(c);val records=queue.pending(System.currentTimeMillis())
  val dropped=queue.dropped()
  if(records.length()==0 && dropped<=prefs(c).getLong("reported_dropped",0))return
  val binding=state.binding!!
  val transport=MdmTransport(c,{store.secret()},wifiOnly={wifiOnly(c)})
  synchronized(lock){
   val latest=store.read()
   if(latest.localGeneration!=state.localGeneration || !MdmTelemetryPolicy.consented(latest)){transport.close();return}
   transfer=transport
  }
  try{
   val body=JSONObject().put("version",1).put("bindingId",binding.id).put("epoch",binding.epoch)
    .put("records",records).put("dropped",dropped)
   val response=JSONObject(transport.post(binding,"telemetry",body.toString()))
   check(response.optBoolean("accepted"))
   synchronized(lock){
    val latest=store.read()
    if(latest.localGeneration==state.localGeneration && MdmTelemetryPolicy.consented(latest)){
     queue.acknowledge((0 until records.length()).map{records.getJSONObject(it).getString("id")}.toSet(),System.currentTimeMillis())
     prefs(c).edit().putLong("last_upload",System.currentTimeMillis()).putLong("reported_dropped",dropped).apply()
     status="Отправлено измерений: "+records.length()
    }
   }
  }finally{synchronized(lock){if(transfer===transport)transfer=null};transport.close()}
 }
 fun lastUpload(c:Context)=prefs(c).getLong("last_upload",0)
}
