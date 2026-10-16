package ru.vpnc.quiclab

import android.content.Context
import android.util.Base64
import mobile.Mobile
import org.json.JSONArray
import org.json.JSONObject
import java.net.URI
import java.net.URL
import java.security.MessageDigest
import java.util.UUID
import javax.net.ssl.HttpsURLConnection

/** Only the encrypted runtime contains URLs and subscription credentials. */
internal object VlessSubscriptions {
 const val MAX_BYTES=1024*1024
 data class Entry(val key:String,val name:String,val endpoint:String,val uri:String)
 data class Parsed(val entries:List<Entry>,val skipped:Int,val availableKeys:Set<String> = entries.map{it.key}.toSet())
 fun url(raw:String):URL {
  require(raw.length in 1..4096)
  val u=URI(raw);require(u.scheme=="https" && !u.host.isNullOrBlank() && u.rawUserInfo==null && u.rawFragment==null && (u.port==-1||u.port in 1..65535))
  return u.toURL()
 }
 fun parse(bytes:ByteArray):Parsed {
  require(bytes.size in 1..MAX_BYTES){"Пустая или слишком большая подписка"}
  var text=bytes.toString(Charsets.UTF_8).trim().removePrefix("\uFEFF")
  if(!text.contains("://") && !text.startsWith("[") && !text.startsWith("{")){
   require(text.matches(Regex("[A-Za-z0-9+/=_\\s-]+"))){"Нужен список VLESS, JSON или Base64"}
   text=try{Base64.decode(text,Base64.DEFAULT).toString(Charsets.UTF_8)}catch(_:IllegalArgumentException){Base64.decode(text,Base64.URL_SAFE).toString(Charsets.UTF_8)}
  }
  val json=text.trimStart().startsWith("[") || text.trimStart().startsWith("{")
  val candidates=mutableListOf<Pair<String,String>>()
  var skipped=0
  if(json){
   validateJSON(text)
   val docs=if(text.trimStart().startsWith("["))JSONArray(text) else JSONArray().put(JSONObject(text))
   require(docs.length() in 1..256){"Не более 256 конфигураций"}
   for(i in 0 until docs.length()){
    val doc=docs.getJSONObject(i)
    val outs=doc.optJSONArray("outbounds")?:JSONArray().put(doc)
    require(outs.length()<=256)
    for(j in 0 until outs.length()){
     val outbound=JSONObject(outs.getJSONObject(j).toString())
     val protocol=outbound.optString("protocol")
     if(protocol=="freedom"||protocol=="blackhole")continue
     if(protocol!="vless"){skipped++;continue}
     // Only an empty TCP settings object is equivalent to the default.
     // All nonempty extensions/chaining remain visible to the strict validator.
     val stream=outbound.optJSONObject("streamSettings")
     if(stream?.optJSONObject("tcpSettings")?.length()==0)stream.remove("tcpSettings")
     candidates.add(outbound.toString() to doc.optString("remarks"))
     require(candidates.size<=256){"Не более 256 подключений"}
    }
   }
  }else{
   val lines=text.lineSequence().map{it.trim()}.filter{it.isNotEmpty()}.toList()
   require(lines.size in 1..256){"Не более 256 записей"}
   candidates.addAll(lines.map{it to ""})
  }
  val entries=mutableListOf<Entry>()
  val configs=mutableMapOf<String,String>()
  for((raw,label) in candidates){
   val info=try{VlessImport.metadata(raw)}catch(_:Exception){skipped++;continue}
   val config=JSONObject(Mobile.importVLESSConfig(raw)).getJSONObject("config")
   val identity=listOf("Endpoint","Security","Flow").joinToString("|"){config.optString(it)}
   val key=MessageDigest.getInstance("SHA-256").digest(identity.toByteArray()).joinToString(""){"%02x".format(it)}
   if(configs.containsKey(key)){
    require(json && configs[key]==config.toString()){"Неоднозначные записи одного сервера"}
    continue
   }
   configs[key]=config.toString()
   entries.add(Entry(key,label.ifBlank{info.optString("name")}.ifBlank{info.getString("endpoint")}.take(90),info.getString("endpoint"),if(json)canonicalURI(config) else raw))
  }
  require(entries.isNotEmpty()){"Нет поддерживаемых VLESS TCP TLS/REALITY записей"}
  return Parsed(entries,skipped)
 }
 private fun canonicalURI(c:JSONObject):String {
  val b=android.net.Uri.Builder().scheme("vless").encodedAuthority(c.getString("UUID")+"@"+c.getString("Endpoint"))
   .appendQueryParameter("type","tcp").appendQueryParameter("encryption","none")
  for((field,param) in listOf("Security" to "security","ServerName" to "sni","Fingerprint" to "fp","Flow" to "flow","RealityPublicKey" to "pbk","ShortID" to "sid","SpiderX" to "spx")){
   val value=c.optString(field);if(value.isNotEmpty())b.appendQueryParameter(param,value)
  }
  return b.build().toString().also{VlessImport.metadata(it)}
 }
 private fun validateJSON(text:String){
  val r=android.util.JsonReader(java.io.StringReader(text))
  fun value(depth:Int){
   require(depth<=24){"Слишком глубокий JSON"}
   when(r.peek()){
    android.util.JsonToken.BEGIN_OBJECT->{r.beginObject();val keys=mutableSetOf<String>();while(r.hasNext()){require(keys.add(r.nextName())){"Повторяющиеся ключи JSON"};value(depth+1)};r.endObject()}
    android.util.JsonToken.BEGIN_ARRAY->{r.beginArray();while(r.hasNext())value(depth+1);r.endArray()}
    android.util.JsonToken.STRING,android.util.JsonToken.NUMBER->r.nextString()
    android.util.JsonToken.BOOLEAN->r.nextBoolean()
    android.util.JsonToken.NULL->r.nextNull()
    else->throw IllegalArgumentException("Неверный JSON")
   }
  }
  r.use{value(0);require(r.peek()==android.util.JsonToken.END_DOCUMENT)}
 }
 internal fun retainedSelection(previous:Set<String>,parsed:Parsed):Set<String> =
  (previous-parsed.availableKeys)+parsed.entries.map{it.key}
 fun fetch(raw:String):Parsed {
  val connection=url(raw).openConnection() as HttpsURLConnection
  connection.instanceFollowRedirects=false;connection.connectTimeout=10000;connection.readTimeout=15000
  connection.setRequestProperty("Accept","text/plain");connection.setRequestProperty("Accept-Encoding","identity")
  val deadline=System.nanoTime()+20_000_000_000L
  try{
   require(connection.responseCode==200){"Сервер не вернул подписку"}
   require(connection.contentLengthLong<=MAX_BYTES){"Подписка слишком большая"}
   val bytes=connection.inputStream.use{input->val out=java.io.ByteArrayOutputStream();val buf=ByteArray(8192)
    while(true){check(!Thread.currentThread().isInterrupted && System.nanoTime()<deadline){"Загрузка отменена или истекло время"};val n=input.read(buf);if(n<0)break;require(out.size()+n<=MAX_BYTES);out.write(buf,0,n)};out.toByteArray()}
   return try{parse(bytes)}finally{bytes.fill(0)}
  }finally{connection.disconnect()}
 }
 fun merge(before:JSONObject,group:String,parsed:Parsed):JSONObject {
  require(parsed.entries.isNotEmpty()){"Выберите хотя бы одно подключение"}
  val result=JSONObject(before.toString());val profiles=result.getJSONArray("profiles")
  for(e in parsed.entries){
   val id=UUID.nameUUIDFromBytes(("vless-subscription:"+group+":"+e.key).toByteArray()).toString()
   val old=(0 until profiles.length()).map{profiles.getJSONObject(it)}.firstOrNull{it.getString("id")==id}
   val p=old?:JSONObject().put("id",id).put("settings",JSONObject().put("transport","vless").put("dns","1.1.1.1").put("routes","0.0.0.0/0").put("mode",0).put("global_apps",true).put("demux_enabled",false))
   if(old==null)p.put("name","VLESS · "+e.name)
   p.getJSONObject("settings").put("endpoint",e.endpoint).put("vless_endpoint",e.endpoint)
   p.put("identity",JSONObject().put("vless_uri",e.uri))
   if(old==null)profiles.put(p)
  }
  require(profiles.length()<=32){"Не более 32 подключений; состав подписки не применён"}
  return result
 }
 fun selected(c:Context,rawURL:String):Set<String> {
  val group=UUID.nameUUIDFromBytes(rawURL.toByteArray()).toString()
  val keys=list(c).optJSONObject(group)?.optJSONArray("selected")?:return emptySet()
  return (0 until keys.length()).map{keys.getString(it)}.toSet()
 }
 fun list(c:Context):JSONObject=MdmConfigurationStore(c).runtime()?.optJSONObject("subscriptions")?:JSONObject()
 fun prepare(c:Context,name:String,rawURL:String,parsed:Parsed):TransferPreview=synchronized(MdmConfiguration.lock){
  MdmConfiguration.assertEditable(c);require(name.isNotBlank()&&name.length<=100);url(rawURL)
  val group=UUID.nameUUIDFromBytes(rawURL.toByteArray()).toString()
  val before=ConfigurationTransfer.snapshot(c,true).toString();val candidate=merge(JSONObject(before),group,parsed)
  Mobile.validateMDMConfiguration(candidate.toString())
  val meta=JSONObject(list(c).toString()).put(group,JSONObject().put("name",name).put("url",rawURL).put("updatedAt",System.currentTimeMillis()).put("selected",JSONArray(retainedSelection(selected(c,rawURL),parsed).toList())))
  val added=candidate.getJSONArray("profiles").length()-JSONObject(before).getJSONArray("profiles").length()
  TransferPreview(candidate.toString(),before,MdmConfiguration.token(c),listOf("Подписка"),emptyList(),
   "Добавлено: $added; обновлено: ${parsed.entries.size-added}; пропущено неподдерживаемых: ${parsed.skipped}.\n"+parsed.entries.joinToString("\n"){it.name}+"\nОтсутствующие в ответе подключения сохраняются. Локальные правила и выбранное подключение сохраняются.",subscriptions=meta.toString())
 }
}
