package ru.vpnc.quiclab

import android.content.Context
import org.json.JSONObject
import java.io.File

/**
 * Encrypted transaction boundary for personal and external snapshots.
 * No SharedPreferences, VPN service, or MDM consent is changed by this store.
 * The runtime adapter must serialize authorization and side effects separately.
 */
internal class MdmConfigurationStore(
 file:File,
 alias:String="quic-lab-mdm-configuration",
 private val beforeWrite:()->Unit={},
) {
 constructor(c:Context):this(File(c.noBackupFilesDir,"mdm-configuration.bin"))
 private val store=MdmStore(file,alias)
 fun effective():JSONObject? {
  val j=store.document()
  return j.optJSONObject("external") ?: j.optJSONObject("personal")
 }
 fun mode():String?=store.document().optString("mode").ifEmpty{null}
 fun revision(binding:String):Long=store.document().let{if(it.optString("owner")==binding)it.optLong("revision") else 0}
 fun initialize(personal:JSONObject,runtime:JSONObject?=null) {
  mobile.Mobile.validateMDMConfiguration(personal.toString())
  store.edit{j->if(!j.has("personal")){beforeWrite();j.put("personal",JSONObject(personal.toString()));if(runtime!=null)j.put("personalRuntime",JSONObject(runtime.toString()))}}
 }
 /** Called only while the controller holds the active generation/right lock. */
 fun apply(binding:String,desired:MdmConfigRevision,compile:((JSONObject,JSONObject)->JSONObject)?=null):Boolean {
  require(binding.isNotBlank() && binding.length<=128 && desired.revision>0)
  require(desired.mode=="current"||desired.mode=="external")
  mobile.Mobile.validateMDMConfiguration(desired.document.toString())
  var applied=false
  store.edit{j->
   check(j.has("personal")){"Личный конфиг ещё не сохранён"}
   val owner=j.optString("owner")
   check(owner.isEmpty() || owner==binding){"Конфиг принадлежит другой привязке MDM"}
   if(owner==binding && desired.revision<=j.optLong("revision"))return@edit
   beforeWrite()
   val next=JSONObject(desired.document.toString())
   val previous=j.optJSONObject("external")?:j.getJSONObject("personal")
   val old=previous.getJSONArray("profiles")
   val previousById=(0 until old.length()).associate{old.getJSONObject(it).let{p->p.getString("id") to p}}
   val profiles=next.getJSONArray("profiles")
   for(i in 0 until profiles.length()){
    val p=profiles.getJSONObject(i)
    if(!p.has("identity") && !p.optBoolean("removeIdentity")){
     previousById[p.getString("id")]?.optJSONObject("identity")?.let{p.put("identity",JSONObject(it.toString()))}
    }
    p.remove("removeIdentity")
   }
   // Check the final merged size as well: preserved credentials count too.
   mobile.Mobile.validateMDMConfiguration(next.toString())
   val runtime=compile?.invoke(JSONObject(desired.document.toString()),j.optJSONObject("externalRuntime")?:j.getJSONObject("personalRuntime"))
   if(runtime!=null){
    if(desired.mode=="external")j.put("externalRuntime",runtime)
    else{j.put("personalRuntime",runtime);j.remove("externalRuntime")}
   }
   j.put("configEpoch",Math.addExact(j.optLong("configEpoch"),1))
   if(desired.mode=="external")j.put("external",next)
   else{j.put("personal",next);j.remove("external")}
   j.put("owner",binding).put("revision",desired.revision).put("mode",desired.mode)
   applied=true
  }
  return applied
 }
 fun runtime():JSONObject?=store.document().let{it.optJSONObject("externalRuntime")?:it.optJSONObject("personalRuntime")}
 fun configEpoch():Long=store.document().optLong("configEpoch")
 fun editRuntime(change:(JSONObject)->Unit){
  store.edit{j->
   check(!j.has("externalRuntime")){"Сначала отключите внешний конфиг MDM"}
   val runtime=j.getJSONObject("personalRuntime");change(runtime);beforeWrite()
  }
 }
 /** Durable, idempotent cleanup. Current remains personal; external disappears. */
 fun detach() {
  store.edit{j->
   beforeWrite()
   j.put("configEpoch",Math.addExact(j.optLong("configEpoch"),1));j.remove("externalRuntime");j.remove("external");j.remove("owner");j.remove("revision");j.remove("mode")
  }
 }
}
