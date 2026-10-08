package ru.vpnc.quiclab

import android.app.job.JobInfo
import android.app.job.JobParameters
import android.app.job.JobScheduler
import android.app.job.JobService
import android.content.ComponentName
import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.util.AtomicFile
import org.json.JSONArray
import org.json.JSONObject
import java.io.ByteArrayOutputStream
import java.io.File
import java.net.URI
import java.security.MessageDigest
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean
import java.util.zip.GZIPOutputStream
import javax.net.ssl.HttpsURLConnection

internal object DiagnosticsDelivery {
 private const val JOB=8147
 private val running=AtomicBoolean()
 private val generation=java.util.concurrent.atomic.AtomicLong()
 private val executor=Executors.newSingleThreadExecutor()
 @Volatile private var nextAttempt=0L
 @Volatile private var failed=false
 @Volatile private var active:HttpsURLConnection?=null
 @Volatile private var manualActive=false
 private data class Cached(val modified:Long,val size:Long,val url:String,val token:String)
 private val destinations=mutableMapOf<String,Cached>()
 @Synchronized fun destination(c:Context,profile:String):Pair<String,String>{
  val file=VpnProfiles.identityFile(c,profile)
  val cached=destinations[profile]
  if(!MdmConfiguration.hasLayer(c)&&cached!=null&&cached.modified==file.lastModified()&&cached.size==file.length())return cached.url to cached.token
  val identity=VpnIdentity.load(c,profile);ServiceTransfer.validateMetadata(identity)
  val id=identity.optString("device_id");require(id.matches(Regex("[a-zA-Z0-9-]{1,64}"))){"Нет регистрации устройства"}
  val uri=URI(identity.getString("config_url"))
  require(uri.path=="/api/v1/devices/$id/config"){"Неизвестный адрес диагностики"}
  val token=identity.getString("update_token");require(token.matches(Regex("[a-fA-F0-9]{64}")))
  val url=URI("https",null,uri.host,uri.port,"/api/v1/devices/$id/diagnostics",null,null).toString()
  destinations[profile]=Cached(file.lastModified(),file.length(),url,token)
  return url to token
 }
 fun schedule(c:Context){
  val scheduler=c.getSystemService(JobScheduler::class.java)
  if(!DiagnosticsPolicy.enabled(c)){scheduler.cancel(JOB);if(!manualActive)active?.disconnect();return}
  val request=NetworkRequest.Builder().addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
   .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED).addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET).build()
  scheduler.schedule(JobInfo.Builder(JOB,ComponentName(c,DiagnosticsUploadJob::class.java))
   .setRequiredNetwork(request).setPeriodic(15*60*1000L).build())
 }
 @Synchronized fun urgent(){if(!failed)nextAttempt=0}
 fun automatic(c:Context,done:()->Unit={}){
  if(!DiagnosticsPolicy.enabled(c)||System.currentTimeMillis()<nextAttempt){done();return}
  send(c,false,done)
 }
 fun manual(c:Context,done:()->Unit={})=send(c,true,done)
 private fun network(c:Context,manual:Boolean):Network?{
  val cm=c.getSystemService(ConnectivityManager::class.java)
  return cm.allNetworks.filter{val caps=cm.getNetworkCapabilities(it)
   caps!=null&&!caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN)&&caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)&&
   (manual || DiagnosticsPolicy.autoAllowed(DiagnosticsPolicy.enabled(c),caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI),caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED),caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)))
  }.sortedBy{if(cm.getNetworkCapabilities(it)?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)==true)0 else 1}.firstOrNull()
 }
 private fun digest(raw:ByteArray)=MessageDigest.getInstance("SHA-256").digest(raw).joinToString(""){"%02x".format(it)}
 private fun pendingDir(c:Context)=File(c.filesDir,"diagnostic-outbox").apply{mkdirs()}
 fun clearPending(c:Context){generation.incrementAndGet();active?.disconnect();pendingDir(c).listFiles()?.forEach{it.delete()}}
 fun pruneOutbox(c:Context){val now=System.currentTimeMillis();pendingDir(c).listFiles()?.forEach{file->val stale=runCatching{val a=JSONObject(file.readText()).getJSONArray("records");(0 until a.length()).any{val r=a.getJSONObject(it);val days=when(r.getString("kind")){"detail"->1;"summary"->3;else->7};r.getLong("time")<now-days*86400000L}}.getOrDefault(true);if(stale)file.delete()}}
 private fun send(context:Context,manual:Boolean,done:()->Unit){
  val c=context.applicationContext
  if(!running.compareAndSet(false,true)){done();return}
  val startedGeneration=generation.get()
  executor.execute{
   manualActive=manual
   val prefs=DiagnosticsPolicy.preferences(c)
   try{
    Diagnostics.init(c)
    val n=network(c,manual)?:run{prefs.edit().putString("status",if(manual)"Нет доступной физической сети" else "Ожидание безлимитного Wi-Fi").apply();return@execute}
    var batches=0
    var failures=0;var lastFailure=""
    for((profile,target)in Diagnostics.store().targets()){
     check(generation.get()==startedGeneration)
     try {
     if(target.isEmpty())continue
     val current=runCatching{destination(c,profile)}.getOrNull()?:continue
     if(current.first!=target)continue // Never reroute old history to another server.
     while(manual || batches<20){
      if(!manual&&!DiagnosticsPolicy.enabled(c))break
      val file=AtomicFile(File(pendingDir(c),digest((profile+"|"+target).toByteArray())+".json"))
      val records=Diagnostics.store().pending(profile,target)
      if(records.isEmpty()) {file.delete();break}
      val retained=runCatching{file.readFully()}.getOrNull()?.takeIf{bytes->runCatching{val a=JSONObject(String(bytes,Charsets.UTF_8)).getJSONArray("records");val ids=records.map{it.id}.toSet();(0 until a.length()).all{a.getJSONObject(it).getString("id") in ids}}.getOrDefault(false)}
      val raw=retained?:JSONObject().put("version",1).put("records",JSONArray().also{a->records.forEach{a.put(it.json())}}).toString().toByteArray().also{
       var stream:java.io.FileOutputStream?=null
       try{stream=file.startWrite();stream.write(it);file.finishWrite(stream)}catch(e:Exception){file.failWrite(stream);throw e}
      }
      // Validate a retained outbox before sending; no arbitrary paths/URLs in it.
      require(raw.size<=1048576)
      upload(c,n,current.first,current.second,raw,manual,startedGeneration)
      val array=JSONObject(String(raw,Charsets.UTF_8)).getJSONArray("records")
      Diagnostics.store().ack((0 until array.length()).map{array.getJSONObject(it).getString("id")})
      file.delete();batches++
      prefs.edit().putLong("last_sent",System.currentTimeMillis()).apply()
     }
     }catch(e:Exception){failures++;lastFailure=if(e is DeliveryError)e.message.orEmpty() else "Ошибка сети или регистрации устройства"}
    }
    if(failures>0)throw DeliveryError("Не доставлено профилей: $failures. $lastFailure")
    failed=false;nextAttempt=System.currentTimeMillis()+300000
    prefs.edit().putLong("retry_delay",60000).putString("status",if(Diagnostics.store().bytes(true)==0L)"Всё доставлено" else if(batches>0)"Часть доставлена; осталось в очереди" else "Нет подходящего адреса доставки для накопленных журналов").apply()
   }catch(e:Exception){
    failed=true
    val delay=prefs.getLong("retry_delay",60000).coerceIn(60000,3600000)
    nextAttempt=System.currentTimeMillis()+delay
    prefs.edit().putLong("retry_delay",(delay*2).coerceAtMost(3600000)).putString("status",if(e is DeliveryError)e.message else "Доставка отложена: проверьте сеть и регистрацию устройства").apply()
   }finally{if(generation.get()!=startedGeneration)pendingDir(c).listFiles()?.forEach{it.delete()};active=null;manualActive=false;running.set(false);done()}
  }
 }
 private class DeliveryError(message:String):Exception(message)
 private fun upload(c:Context,n:Network,url:String,token:String,raw:ByteArray,manual:Boolean,startedGeneration:Long){
  val cm=c.getSystemService(ConnectivityManager::class.java)
  fun allowed():Boolean{
   if(generation.get()!=startedGeneration)return false
   val caps=cm.getNetworkCapabilities(n)?:return false
   if(caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN))return false
   return manual||DiagnosticsPolicy.autoAllowed(DiagnosticsPolicy.enabled(c),caps.hasTransport(NetworkCapabilities.TRANSPORT_WIFI),caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_METERED),caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED))
  }
  check(allowed())
  val bytes=ByteArrayOutputStream().also{out->GZIPOutputStream(out).use{it.write(raw)}}.toByteArray()
  require(bytes.size<=262144)
  val connection=n.openConnection(java.net.URL(url)) as HttpsURLConnection
  active=connection
  val callback=object:ConnectivityManager.NetworkCallback(){
   override fun onLost(network:Network){if(network==n)connection.disconnect()}
   override fun onCapabilitiesChanged(network:Network,caps:NetworkCapabilities){if(network==n&&!allowed())connection.disconnect()}
  }
  cm.registerNetworkCallback(NetworkRequest.Builder().addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET).build(),callback)
  try{
   connection.instanceFollowRedirects=false;connection.connectTimeout=15000;connection.readTimeout=20000
   connection.requestMethod="POST";connection.doOutput=true
   connection.setRequestProperty("Authorization","Bearer $token");connection.setRequestProperty("Content-Encoding","gzip")
   connection.setRequestProperty("Content-Type","application/json");connection.setFixedLengthStreamingMode(bytes.size)
   connection.outputStream.use{out->var pos=0;while(pos<bytes.size){check(allowed());val size=minOf(8192,bytes.size-pos);out.write(bytes,pos,size);pos+=size}}
   val code=connection.responseCode;if(code!=200)throw DeliveryError("Сервер отклонил загрузку (HTTP $code)")
   val response=connection.inputStream.use{input->val output=ByteArrayOutputStream();val buffer=ByteArray(1024);while(output.size()<=4096){val count=input.read(buffer,0,minOf(buffer.size,4097-output.size()));if(count<0)break;output.write(buffer,0,count)};output.toByteArray()};require(response.size<=4096)
   val ack=JSONObject(String(response,Charsets.UTF_8))
   check(ack.optBoolean("accepted")&&ack.optString("batch")==digest(raw)){"Неверное подтверждение"}
  }finally{cm.unregisterNetworkCallback(callback);connection.disconnect();active=null}
 }
}
class DiagnosticsUploadJob:JobService(){
 override fun onStartJob(params:JobParameters):Boolean{
  Diagnostics.init(this)
  android.os.Handler(mainLooper).post{DiagnosticsDelivery.automatic(this){jobFinished(params,false)}}
  return true
 }
 override fun onStopJob(params:JobParameters)=true
}
