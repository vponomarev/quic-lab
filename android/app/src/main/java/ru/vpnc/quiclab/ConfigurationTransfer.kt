package ru.vpnc.quiclab

import android.content.Context
import android.net.Uri
import org.json.JSONArray
import org.json.JSONObject
import mobile.Mobile

internal data class TransferPreview(val candidate:String,val before:String,val token:Pair<Long,Long>,val sections:List<String>,val missing:List<String>,val summary:String,val rollbackRuntime:String?=null,val missingApps:List<String> = emptyList())
internal object ConfigurationTransfer {
 fun decode(bytes:ByteArray,password:String):String {
  require(bytes.size<=2*1024*1024){"Файл слишком большой"}
  val encrypted=bytes.take(22).toByteArray().toString(Charsets.US_ASCII).startsWith("age-encryption.org/v1")
  val raw=if(encrypted)Mobile.decryptConfiguration(bytes,password).toString(Charsets.UTF_8)else bytes.toString(Charsets.UTF_8)
  require(encrypted || JSONObject(raw).optString("credentials")=="omitted"){"Конфиг с ключами должен быть зашифрован"}
  return raw
 }
 fun snapshot(c:Context,secrets:Boolean):JSONObject=synchronized(MdmConfiguration.lock){
  val doc=MdmConfiguration.snapshotLocal(c)
  if(secrets){
   val ps=doc.getJSONArray("profiles")
   for(i in 0 until ps.length()){
    val p=ps.getJSONObject(i);val id=p.getString("id")
    if(!VpnIdentity.exists(c,id))continue
    val stored=VpnIdentity.load(c,id);val identity=JSONObject()
    for(k in listOf("certificate","key","awg_config"))if(stored.has(k))identity.put(k,stored.getString(k))
    if(stored.has("vless_config")){
     val v=JSONObject(stored.getString("vless_config")).getJSONObject("config")
     require(v.optString("RootPEM").let{it.isEmpty()||it=="null"}){"VLESS с частным CA пока не поддерживает перенос"}
     val uri=Uri.Builder().scheme("vless").encodedAuthority(v.getString("UUID")+"@"+v.getString("Endpoint"))
      .appendQueryParameter("type","tcp").appendQueryParameter("security",v.getString("Security"))
      .appendQueryParameter("sni",v.getString("ServerName"))
     for((a,b) in mapOf("Fingerprint" to "fp","Flow" to "flow","RealityPublicKey" to "pbk","ShortID" to "sid","SpiderX" to "spx"))v.optString(a).takeIf{it.isNotEmpty()}?.let{uri.appendQueryParameter(b,it)}
     identity.put("vless_uri",uri.build().toString())
    }
    if(identity.length()>0)p.put("identity",identity)
   }
  }
  doc
 }
 fun export(c:Context,sections:List<String>,profiles:List<String>,secrets:Boolean,password:String):ByteArray {
  val selection=JSONObject().put("sections",JSONArray(sections)).put("profileIds",JSONArray(profiles))
  val raw=Mobile.exportConfiguration(snapshot(c,secrets).toString(),selection.toString(),secrets).toByteArray()
  return if(secrets){require(password.length>=8){"Пароль: минимум 8 символов"};Mobile.encryptConfiguration(raw,password)}else raw
 }
 fun prepare(c:Context,raw:String,mode:String,mapping:Map<String,String>):TransferPreview=synchronized(MdmConfiguration.lock){
  MdmConfiguration.assertEditable(c)
  val before=snapshot(c,true).toString();val token=MdmConfiguration.token(c)
  val result=JSONObject(Mobile.prepareConfigurationImport(before,raw,JSONObject().put("mode",mode).put("mapping",JSONObject(mapping)).toString()))
  val candidate=result.getJSONObject("candidate")
  val sections=result.getJSONArray("sections").let{a->(0 until a.length()).map{a.getString(it)}}
  val missing=result.getJSONArray("missingCredentials").let{a->(0 until a.length()).map{a.getString(it)}}
  val old=JSONObject(before).getJSONArray("profiles");val next=candidate.getJSONArray("profiles")
  val names=(0 until next.length()).joinToString("\n"){next.getJSONObject(it).getString("name")}
  val absent=missingApplications(c,candidate)
  val details=describe(JSONObject(before),candidate)+if(absent.isEmpty())"" else "\nНе установлены приложения: ${absent.joinToString()}. Установите их или исправьте исходный конфиг перед применением."
  val summary="Разделы: ${sections.joinToString{sectionName(it)}}\nПодключений: ${old.length()} → ${next.length()}\n$names"+
   if(missing.isNotEmpty())"\nБез ключей: ${missing.size}. Эти подключения останутся выключенными."else ""
  TransferPreview(candidate.toString(),before,token,sections,missing,summary+"\n"+details,missingApps=absent)
 }
 fun rollback(c:Context):TransferPreview=synchronized(MdmConfiguration.lock){
  MdmConfiguration.assertEditable(c)
  val saved=MdmConfigurationStore(c).localRollback()?:error("Нет предыдущего импорта для отката")
  val before=snapshot(c,true).toString();val doc=saved.getJSONObject("document")
  TransferPreview(doc.toString(),before,MdmConfiguration.token(c),listOf("Откат"),emptyList(),describe(JSONObject(before),doc),saved.getJSONObject("runtime").toString())
 }
 private fun describe(before:JSONObject,after:JSONObject):String {
  val lines=mutableListOf<String>()
  val a=before.getJSONArray("profiles");val old=(0 until a.length()).associate{a.getJSONObject(it).let{p->p.getString("id") to p}}
  val b=after.getJSONArray("profiles");val next=(0 until b.length()).associate{b.getJSONObject(it).let{p->p.getString("id") to p}}
  for((id,p) in old)if(id !in next)lines.add("Удалить: ${p.getString("name")}")
  for((id,p) in next){
   val prev=old[id]
   if(prev==null){lines.add("Добавить: ${p.getString("name")}");continue}
   if(prev.getString("name")!=p.getString("name"))lines.add("Название: ${prev.getString("name")} → ${p.getString("name")}")
   val x=prev.getJSONObject("settings");val y=p.getJSONObject("settings")
   for(key in (x.keys().asSequence().toSet()+y.keys().asSequence().toSet()).sorted()){
    if(x.opt(key).toString()!=y.opt(key).toString())lines.add("${p.getString("name")} · $key: "+if(key=="ca")"сертификат изменён" else "${x.opt(key).toString().take(100)} → ${y.opt(key).toString().take(100)}")
   }
   if(prev.optJSONObject("identity")?.toString()!=p.optJSONObject("identity")?.toString())lines.add("${p.getString("name")}: ключи изменены")
  }
  for(key in listOf("currentProfileId","enabledProfileIds","multiple","globalApps","dns","budget","reserve","diagnostics")){
   if(before.opt(key).toString()!=after.opt(key).toString())lines.add("$key: ${before.opt(key).toString().take(200)} → ${after.opt(key).toString().take(200)}")
  }
  return if(lines.isEmpty())"Настройки совпадают" else lines.joinToString("\n")
 } internal fun missingApplications(c:Context,doc:JSONObject):List<String>{
  val packages=mutableSetOf<String>()
  fun add(a:org.json.JSONArray?){if(a!=null)for(i in 0 until a.length())packages.add(a.getString(i))}
  add(doc.optJSONArray("globalApps"))
  val profiles=doc.getJSONArray("profiles")
  for(i in 0 until profiles.length())add(profiles.getJSONObject(i).getJSONObject("settings").optJSONArray("apps"))
  return packages.filter{runCatching{c.packageManager.getPackageInfo(it,0)}.isFailure}.sorted()
 } private fun sectionName(s:String)=when(s){"profiles"->"подключения";"routing"->"маршруты и приложения";"network"->"сети и лимит";"diagnostics"->"диагностика";else->s}}
