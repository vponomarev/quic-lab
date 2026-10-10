package ru.vpnc.quiclab

import android.content.Context
import android.content.Intent
import android.net.VpnService
import org.json.JSONObject

internal object LocalConfigurationApply {
 fun canRestart(wasRunning:Boolean,oldStop:Long,currentStop:Long,incomplete:Boolean)=wasRunning&&oldStop==currentStop&&!incomplete
 fun apply(c:Context,p:TransferPreview,vpn:MdmVpnPort=Port(c))=MdmApplyCoordinator.onMain {
  synchronized(MdmConfiguration.lock){
   MdmConfiguration.assertEditable(c,p.token)
   check(ConfigurationTransfer.snapshot(c,true).toString()==p.before){"Настройки изменились. Подготовьте импорт заново."}
   val store=MdmConfigurationStore(c);val doc=JSONObject(p.candidate)
   check(ConfigurationTransfer.missingApplications(c,doc).isEmpty()){"Установите отсутствующие приложения или исправьте конфиг перед применением"}
   val previous=MdmConfiguration.snapshotRuntime(c)
   val runtime=p.rollbackRuntime?.let{JSONObject(it)}?:MdmConfiguration.compile(doc,previous)
   // Explicitly matched profiles retain private update/diagnostic metadata.
   val beforeProfiles=JSONObject(p.before).getJSONArray("profiles")
   val before=(0 until beforeProfiles.length()).associate{beforeProfiles.getJSONObject(it).let{it.getString("id") to it}}
   val profiles=doc.getJSONArray("profiles")
   val oldPrefs=previous.getJSONObject("preferences");val newPrefs=runtime.getJSONObject("preferences")
   if(p.rollbackRuntime==null)for(i in 0 until profiles.length()){
    val profile=profiles.getJSONObject(i);val id=profile.getString("id");val old=before[id]?:continue
    val name=if(id=="default")"vpn" else "vpn_$id"
    val oldValues=oldPrefs.optJSONObject(name)?:JSONObject();val portable=old.getJSONObject("settings")
    oldValues.keys().asSequence().filter{!portable.has(it)}.forEach{newPrefs.getJSONObject(name).put(it,oldValues.get(it))}
    if(old.optJSONObject("identity")?.toString()==profile.optJSONObject("identity")?.toString()){
     previous.getJSONObject("identities").optJSONObject(id)?.let{runtime.getJSONObject("identities").put(id,it)}
    }
   }
   if(!MdmConfiguration.hasLayer(c))store.initialize(JSONObject(p.before),previous)
   val stop=MdmStore(c).document().optLong("user_stop")
   val incomplete=p.missing.contains(doc.getString("currentProfileId")) || (doc.getJSONObject("dns").getString("mode")=="tunnel" && p.missing.contains(doc.getJSONObject("dns").getString("profileId")))
   val op=JSONObject().put("wasRunning",vpn.active()).put("userStop",stop)
    .put("generation",p.token.first).put("boot",android.provider.Settings.Global.getInt(c.contentResolver,android.provider.Settings.Global.BOOT_COUNT,-1))
    .put("incomplete",incomplete).put("epoch",store.configEpoch())
   store.beginLocalOperation(op)
   try{
    if(vpn.active())vpn.stop()
    MdmConfiguration.assertEditable(c,p.token)
    store.applyLocal(doc,runtime,p.token.second,JSONObject(p.before))
    if(incomplete)VpnBudgetRun.sharedForContext(c).setResumeEligible(false)
   }finally{recover(c,vpn)}
  }
 }
 fun recover(c:Context,vpn:MdmVpnPort=Port(c)){
  synchronized(MdmConfiguration.lock){
   val store=MdmConfigurationStore(c);val op=store.localOperation()?:return
   val state=MdmStore(c).read()
   val boot=android.provider.Settings.Global.getInt(c.contentResolver,android.provider.Settings.Global.BOOT_COUNT,-1)
   val committed=store.configEpoch()>op.optLong("epoch")
   if(committed && op.optBoolean("incomplete"))VpnBudgetRun.sharedForContext(c).setResumeEligible(false)
   if(!state.cleanupPending && !(state.active&&state.rights.config) && state.localGeneration==op.optLong("generation") && boot==op.optInt("boot") &&
    canRestart(op.optBoolean("wasRunning"),op.optLong("userStop"),MdmStore(c).document().optLong("user_stop"),committed&&op.optBoolean("incomplete")) && !vpn.active()){
     vpn.start()
   }
   store.finishLocalOperation()
  }
 }
 private class Port(private val c:Context):MdmVpnPort {
  override fun active()=LabVpnService.active
  override fun stop(){LabVpnService.stopForMdm()}
  override fun start():String {
   if(VpnService.prepare(c)!=null)return "permission_required"
   c.startForegroundService(Intent(c,LabVpnService::class.java).putExtra("mdm_restart",true).putExtra("mdm_user_stop",MdmStore(c).document().optLong("user_stop")))
   return "starting"
  }
 }}
