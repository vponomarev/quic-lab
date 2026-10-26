package ru.vpnc.quiclab
import org.json.JSONObject
internal object MdmProfileSelection {
 fun allows(runtime:JSONObject?,id:String):Boolean {
  if(runtime==null)return false
  val meta=runtime.optJSONObject("preferences")?.optJSONObject("vpn_profiles")?:return false
  val policy=runtime.optJSONObject("profileSelection")?:return false
  val ids=policy.optJSONArray("profileIds")?:return false
  return !meta.optBoolean("multiple") && policy.optBoolean("enabled") && (0 until ids.length()).any{ids.optString(it)==id}
 }
 private fun settings(runtime:JSONObject,id:String)=runtime.optJSONObject("preferences")?.optJSONObject(if(id=="default")"vpn" else "vpn_$id")
 private fun endpoint(p:JSONObject,kind:String)=p.optString("${kind}_endpoint").ifBlank{if(p.optString("transport")==kind)p.optString("endpoint") else ""}
 fun transports(runtime:JSONObject?,id:String):List<String>{
  if(!allows(runtime,id))return emptyList()
  val p=runtime!!.optJSONObject("localTransports")?.optJSONObject(id)?.optJSONObject("baseline")?:settings(runtime,id)?:return emptyList()
  val keys=runtime.optJSONObject("identities")?.optJSONObject(id)?:return emptyList()
  val declared=p.optJSONArray("available_transports")
  val fallback=if(p.optString("transport") in listOf("quic","https"))listOf("quic","https") else listOf(p.optString("transport"))
  return listOf("quic","https","awg").filter{kind->
   val available=if(declared==null)kind in fallback else (0 until declared.length()).any{declared.optString(it)==kind}
   available && endpoint(p,kind).isNotBlank() && when(kind){
    "awg"->keys.optString("awg_config").isNotBlank()
    else->keys.optString("certificate").isNotBlank() && keys.optString("key").isNotBlank()
   }
  }
 }
 fun selectTransport(runtime:JSONObject,id:String,kind:String){
  check(kind in transports(runtime,id)){"MDM не разрешает этот транспорт или в профиле нет адреса/ключей"}
  val p=settings(runtime,id)!!
  val choices=runtime.optJSONObject("localTransports")?:JSONObject().also{runtime.put("localTransports",it)}
  val previous=choices.optJSONObject(id)
  val baseline=previous?.optJSONObject("baseline")?:JSONObject(p.toString())
  choices.put(id,JSONObject().put("transport",kind).put("baseline",baseline)
   .put("identity",JSONObject(runtime.getJSONObject("identities").getJSONObject(id).toString())))
  p.put("endpoint",endpoint(baseline,kind)).put("transport",kind)
 }
 private fun same(a:Any?,b:Any?):Boolean{
  if(a is JSONObject && b is JSONObject){
   val ka=a.keys().asSequence().toSet();val kb=b.keys().asSequence().toSet()
   return ka==kb && ka.all{same(a.opt(it),b.opt(it))}
  }
  if(a is org.json.JSONArray && b is org.json.JSONArray)return a.length()==b.length() && (0 until a.length()).all{same(a.opt(it),b.opt(it))}
  return a==b
 }
 fun carry(doc:JSONObject,previous:JSONObject,next:JSONObject) {
  val policy=doc.optJSONObject("profileSelection")
  if(policy!=null)next.put("profileSelection",JSONObject(policy.toString()))
  val server=doc.getString("currentProfileId")
  next.put("serverSelectedProfile",server)
  val old=previous.optJSONObject("profileSelection")
  val selected=previous.optJSONObject("preferences")?.optJSONObject("vpn_profiles")?.optString("current").orEmpty()
  if(old?.optBoolean("enabled")==true && policy!=null && previous.optString("serverSelectedProfile")==server && old.optLong("serverRevision")==policy.optLong("serverRevision") && allows(next,selected)){
   next.getJSONObject("preferences").getJSONObject("vpn_profiles").put("current",selected)
  }
  if(old?.optBoolean("enabled")==true && policy?.optBoolean("enabled")==true && previous.optString("serverSelectedProfile")==server && old.optLong("serverRevision")==policy.optLong("serverRevision")){
   val choices=previous.optJSONObject("localTransports")
   choices?.keys()?.asSequence()?.toList()?.forEach{id->
    val choice=choices.getJSONObject(id);val kind=choice.optString("transport")
    if(same(choice.optJSONObject("baseline"),settings(next,id)) && same(choice.optJSONObject("identity"),next.optJSONObject("identities")?.optJSONObject(id)) && kind in transports(next,id))selectTransport(next,id,kind)
   }
  }
 }
}
