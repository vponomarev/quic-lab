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
     // Mux changes client-side stream reuse, not the reachability/authentication
     // of an individual reserve node. The probe tests each outbound separately.
     outbound.remove("mux")
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
   // Keep the subscription profile identity stable when a provider rotates a client UUID.
   // ProbeSource fingerprints the complete config separately, so in-flight results still expire.
   val identity=listOf("Endpoint","Security","Flow","Transport","WebSocketPath","WebSocketHost","RealityPublicKey","ShortID","SpiderX").joinToString("|"){config.optString(it)}
   val key=MessageDigest.getInstance("SHA-256").digest(identity.toByteArray()).joinToString(""){"%02x".format(it)}
   if(configs.containsKey(key)){
    require(json && configs[key]==config.toString()){"Неоднозначные записи одного сервера"}
    continue
   }
   configs[key]=config.toString()
   entries.add(Entry(key,label.ifBlank{info.optString("name")}.ifBlank{info.getString("endpoint")}.take(90),info.getString("endpoint"),if(json)canonicalURI(config) else raw))
  }
  require(entries.isNotEmpty()){"Нет поддерживаемых VLESS TCP TLS/REALITY или WebSocket/TLS записей"}
  return Parsed(entries,skipped)
 }
 private fun canonicalURI(c:JSONObject):String {
  val b=android.net.Uri.Builder().scheme("vless").encodedAuthority(c.getString("UUID")+"@"+c.getString("Endpoint"))
   .appendQueryParameter("type",c.optString("Transport").ifBlank{"tcp"}).appendQueryParameter("encryption","none")
  for((field,param) in listOf("Security" to "security","ServerName" to "sni","Fingerprint" to "fp","Flow" to "flow","RealityPublicKey" to "pbk","ShortID" to "sid","SpiderX" to "spx")){
   val value=c.optString(field);if(value.isNotEmpty())b.appendQueryParameter(param,value)
  }
  if(c.optString("Transport")=="ws"){
   if(c.optString("WebSocketPath").isNotEmpty())b.appendQueryParameter("path",c.getString("WebSocketPath"))
   if(c.optString("WebSocketHost").isNotEmpty())b.appendQueryParameter("host",c.getString("WebSocketHost"))
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
 private fun profileID(group:String,key:String)=UUID.nameUUIDFromBytes(("vless-subscription:"+group+":"+key).toByteArray()).toString()
 /** Only canonical node identity for membership matching, never an export of credentials/CA. */
 internal fun membershipSnapshot(identities:Map<String,JSONObject>):JSONObject {
  val profiles=JSONArray()
  for((id,stored) in identities)runCatching {
   val identity=JSONObject();val uris=stored.optJSONArray("vless_uris")
   if(uris!=null && uris.length()>0)identity.put("vless_uris",JSONArray(uris.toString())).put("vless_uri",uris.getString(0))
   else if(stored.has("vless_config"))identity.put("vless_uri",canonicalURI(JSONObject(stored.getString("vless_config")).getJSONObject("config")))
   if(identity.length()>0)profiles.put(JSONObject().put("id",id).put("identity",identity))
  }
  return JSONObject().put("profiles",profiles)
 }
 internal fun membershipSnapshot(c:Context):JSONObject=synchronized(MdmConfiguration.lock){
  membershipSnapshot(VpnProfiles.list(c).associate{it.id to runCatching{VpnIdentity.load(c,it.id)}.getOrElse{JSONObject()}})
 }
 private fun legacyKey(raw:String):String {
  val config=JSONObject(Mobile.importVLESSConfig(raw)).getJSONObject("config")
  return MessageDigest.getInstance("SHA-256").digest(listOf("Endpoint","Security","Flow").joinToString("|"){config.optString(it)}.toByteArray()).joinToString(""){"%02x".format(it)}
 }
 /** Ownership is checked against both ID schemes before a stored URI can match an entry. */
 internal fun profileKeys(before:JSONObject,group:String):Map<String,String> {
  val profiles=before.getJSONArray("profiles");val result=mutableMapOf<String,String>()
  for(i in 0 until profiles.length())runCatching {
   val p=profiles.getJSONObject(i);val raw=p.getJSONObject("identity").getString("vless_uri")
   val entry=parse(raw.toByteArray()).entries.single()
   val legacy=legacyKey(raw)
   val id=p.getString("id")
   if(id==profileID(group,entry.key)||id==profileID(group,legacy))result[id]=entry.key
  }
  return result
 }
 internal fun normalizeSelected(before:JSONObject,group:String,keys:Set<String>):Set<String> {
  val aliases=mutableMapOf<String,MutableSet<String>>();val profiles=before.getJSONArray("profiles")
  val owned=profileKeys(before,group)
  val groupId=UUID.nameUUIDFromBytes(("vless-group:"+group).toByteArray()).toString()
  for(i in 0 until profiles.length())runCatching {
   val p=profiles.getJSONObject(i);val id=p.getString("id")
   if(id !in owned && id!=groupId)return@runCatching
   val identity=p.getJSONObject("identity");val uris=identity.optJSONArray("vless_uris")
   val raws=if(uris==null)listOf(identity.getString("vless_uri"))else (0 until uris.length()).map{uris.getString(it)}
   for(raw in raws){val key=parse(raw.toByteArray()).entries.single().key;aliases.getOrPut(legacyKey(raw)){mutableSetOf()}.add(key)}
  }
  return keys.map{aliases[it]?.singleOrNull()?:it}.toSet()
 }
 internal fun subscriptionProfileIds(before:JSONObject,group:String,selected:Set<String>):Set<String> {
  val normalized=normalizeSelected(before,group,selected)
  return profileKeys(before,group).filterValues{it in normalized}.keys
 }
 fun merge(before:JSONObject,group:String,parsed:Parsed):JSONObject {
  require(parsed.entries.isNotEmpty()){"Выберите хотя бы одно подключение"}
  val result=JSONObject(before.toString());val profiles=result.getJSONArray("profiles")
  val owned=profileKeys(before,group)
  for(e in parsed.entries){
   val matches=owned.filterValues{it==e.key}.keys
   require(matches.size<=1){"Неоднозначные сохранённые подключения подписки"}
   val id=matches.singleOrNull()?:profileID(group,e.key)
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
 fun mergeGroup(before:JSONObject,group:String,name:String,parsed:Parsed):JSONObject {
  require(parsed.entries.size in 1..256){"Выберите от 1 до 256 серверов группы"}
  val result=JSONObject(before.toString());val profiles=result.getJSONArray("profiles")
  val id=UUID.nameUUIDFromBytes(("vless-group:"+group).toByteArray()).toString()
  val old=(0 until profiles.length()).map{profiles.getJSONObject(it)}.firstOrNull{it.getString("id")==id}
  val p=old?:JSONObject().put("id",id).put("name",("VLESS · "+name+" · группа").take(100))
   .put("settings",JSONObject().put("transport","vless").put("dns","1.1.1.1").put("routes","0.0.0.0/0").put("mode",0).put("global_apps",true).put("demux_enabled",false))
  p.getJSONObject("settings").put("endpoint",parsed.entries.first().endpoint).put("vless_endpoint",parsed.entries.first().endpoint)
  val previous=old?.optJSONObject("identity")?.optJSONArray("vless_uris")
  val retained=if(previous==null)emptyList()else (0 until previous.length()).map{previous.getString(it)}.filter{raw->parse(raw.toByteArray()).entries.single().key !in parsed.availableKeys}
  val members=parsed.entries.map{it.uri}+retained
  require(members.size<=256){"Не более 256 серверов, включая временно отсутствующие"}
  p.put("identity",JSONObject().put("vless_uri",members.first()).put("vless_uris",JSONArray(members)))
  if(old==null)profiles.put(p)
  require(profiles.length()<=32)
  return result
 }
 fun selected(c:Context,rawURL:String):Set<String> {
  val group=UUID.nameUUIDFromBytes(rawURL.toByteArray()).toString()
  val keys=list(c).optJSONObject(group)?.optJSONArray("selected")?:return emptySet()
  return normalizeSelected(membershipSnapshot(c),group,(0 until keys.length()).map{keys.getString(it)}.toSet())
 }
 fun list(c:Context):JSONObject=MdmConfigurationStore(c).runtime()?.optJSONObject("subscriptions")?:JSONObject()
 fun prepare(c:Context,name:String,rawURL:String,parsed:Parsed,asGroup:Boolean=false):TransferPreview=synchronized(MdmConfiguration.lock){
  MdmConfiguration.assertEditable(c);require(name.isNotBlank()&&name.length<=100);url(rawURL)
  val group=UUID.nameUUIDFromBytes(rawURL.toByteArray()).toString()
  val before=ConfigurationTransfer.snapshot(c,true).toString();val candidate=if(asGroup)mergeGroup(JSONObject(before),group,name,parsed)else merge(JSONObject(before),group,parsed)
  Mobile.validateMDMConfiguration(candidate.toString())
  val meta=JSONObject(list(c).toString()).put(group,JSONObject().put("name",name).put("url",rawURL).put("asGroup",asGroup).put("updatedAt",System.currentTimeMillis()).put("selected",JSONArray(retainedSelection(selected(c,rawURL),parsed).toList())))
  val added=candidate.getJSONArray("profiles").length()-JSONObject(before).getJSONArray("profiles").length()
  TransferPreview(candidate.toString(),before,MdmConfiguration.token(c),listOf("Подписка"),emptyList(),
   "${if(asGroup) "Единая группа: ${parsed.entries.size} серверов. " else ""}Добавлено: $added; обновлено: ${(if(asGroup)1 else parsed.entries.size)-added}; пропущено неподдерживаемых: ${parsed.skipped}.\n"+parsed.entries.joinToString("\n"){it.name}+"\nОтсутствующие в ответе подключения сохраняются. Локальные правила и выбранное подключение сохраняются.",subscriptions=meta.toString())
 }
}
