package ru.vpnc.quiclab

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import org.json.JSONArray
import org.json.JSONObject
import java.net.URI
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean
import javax.net.ssl.HttpsURLConnection

/** Public metadata only; never fetches or applies a VPN configuration. */
internal object ApplicationUpdates {
 data class Offer(val profile:String,val server:String,val name:String,val code:Int,val url:String)
 private val executor=Executors.newSingleThreadExecutor()
 private val busy=AtomicBoolean()
 private fun prefs(c:Context)=c.getSharedPreferences("application_updates",Context.MODE_PRIVATE)
 fun parse(profile:String,server:String,caps:JSONObject):Offer? {
  val code=caps.optInt("android_version_code",0)
  val name=caps.optString("android_version_name")
  val url=caps.optString("apk_url")
  if(code<=0||name.isBlank()||url.isBlank())return null
  require(name.length<=128)
  ServiceTransfer.validateMetadata(JSONObject().put("apk_url",url))
  return Offer(profile,server,name,code,url)
 }
 fun offers(c:Context):List<Offer> {
  val valid=VpnProfiles.list(c).map{it.id}.toSet()
  return runCatching {
   val array=JSONArray(prefs(c).getString("offers","[]"))
   (0 until array.length()).mapNotNull{val o=array.getJSONObject(it);parse(o.getString("profile"),o.getString("server"),o)}
    .filter{it.profile in valid}.sortedByDescending{it.code}
  }.getOrDefault(emptyList())
 }
 fun newer(c:Context)=offers(c).any{it.code>BuildConfig.VERSION_CODE}
 fun checking()=busy.get()
 fun status(c:Context)=if(busy.get())"Проверяем обновления…" else prefs(c).getString("status","Версия на сервере ещё не проверена").orEmpty()
 fun check(context:Context,force:Boolean=false,done:()->Unit={}) {
  val c=context.applicationContext
  if(!force&&System.currentTimeMillis()-prefs(c).getLong("checked",0)<3600000){done();return}
  if(!busy.compareAndSet(false,true)){done();return}
  executor.execute {
   try {
    val cm=c.getSystemService(ConnectivityManager::class.java)
    val network=cm.allNetworks.firstOrNull{n->cm.getNetworkCapabilities(n)?.let{!it.hasTransport(NetworkCapabilities.TRANSPORT_VPN)&&it.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)}==true}
      ?:error("Нет доступной сети")
    val result=JSONArray();var checked=0;val failures=mutableListOf<String>()
    for(profile in VpnProfiles.list(c)){
     val base=runCatching{URI(DiagnosticsDelivery.destination(c,profile.id).first)}.getOrNull() ?: continue
     val endpoint=URI("https",null,base.host,base.port,"/api/v1/capabilities",null,null)
     val connection=network.openConnection(endpoint.toURL()) as HttpsURLConnection
     try {
      connection.instanceFollowRedirects=false;connection.connectTimeout=8000;connection.readTimeout=8000
      val http=connection.responseCode;if(http!=200)throw UpdateHttpException(http)
      val raw=connection.inputStream.use{it.readBytesBounded(65536)}
      val offer=parse(profile.id,base.authority,JSONObject(String(raw,Charsets.UTF_8)))
      checked++
      if(offer!=null)result.put(JSONObject().put("profile",offer.profile).put("server",offer.server)
       .put("android_version_name",offer.name).put("android_version_code",offer.code).put("apk_url",offer.url))
     }catch(e:Exception){failures.add(base.authority+": "+failureReason(e))}finally{connection.disconnect()}
    }
    prefs(c).edit().putString("offers",result.toString()).putLong("checked",System.currentTimeMillis())
     .putString("status",when{failures.isNotEmpty()->"Не удалось проверить версию приложения:\n"+failures.joinToString("\n")+"\nЭто не проверка работоспособности VPN."
      checked==0->"Нет зарегистрированных профилей с сервером обновления"
      result.length()==0->"Серверы пока не сообщают версию APK"
      else->if((0 until result.length()).any{result.getJSONObject(it).getInt("android_version_code")>BuildConfig.VERSION_CODE})"Доступна новая версия приложения" else "Обновлений нет: установлена актуальная версия"}+"\nПроверено: "+checkTime()).apply()
   }catch(e:Exception){prefs(c).edit().putLong("checked",System.currentTimeMillis()).putString("status","Не удалось проверить версию приложения: "+failureReason(e)+"\nПроверено: "+checkTime()).apply()}
   finally{busy.set(false);done()}
  }
 }
 private fun checkTime()=java.time.Instant.now().atZone(java.time.ZoneId.systemDefault()).format(java.time.format.DateTimeFormatter.ofPattern("dd.MM HH:mm:ss"))
 private class UpdateHttpException(val code:Int):Exception()
 internal fun failureReason(e:Exception):String=when(e){
  is UpdateHttpException->"сервер ответил HTTP ${e.code}"
  is java.net.UnknownHostException->"не удалось определить IP сервера (DNS)"
  is java.net.SocketTimeoutException->"истекло время ожидания ответа"
  is javax.net.ssl.SSLException->"не удалось установить защищённое соединение (TLS)"
  is java.net.ConnectException->"не удалось подключиться к серверу"
  is java.io.IOException->"ошибка передачи данных"
  is IllegalStateException->"нет доступной сети с подтверждённым доступом в интернет"
  else->"сервер вернул некорректные сведения об обновлении"
 }
 private fun java.io.InputStream.readBytesBounded(max:Int):ByteArray {
  val out=java.io.ByteArrayOutputStream();val buffer=ByteArray(4096)
  while(true){val n=read(buffer,0,minOf(buffer.size,max+1-out.size()));if(n<0)break;out.write(buffer,0,n);require(out.size()<=max)}
  return out.toByteArray()
 }
}
