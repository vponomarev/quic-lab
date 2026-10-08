package ru.vpnc.quiclab

import android.content.Context
import android.content.Intent
import org.json.JSONArray
import org.json.JSONObject
import java.security.MessageDigest

internal object MdmDeviceReport {
 private fun hash(s:String)=MessageDigest.getInstance("SHA-256").digest(s.toByteArray()).joinToString(""){"%02x".format(it)}
 fun configurationGeneration(c:Context):Long=synchronized(MdmConfiguration.lock){
  val store=MdmStore(c);val fingerprint=hash(MdmConfiguration.snapshotLocal(c).toString())
  store.edit{j->if(j.optString("report_config_hash")!=fingerprint)j.put("report_config_hash",fingerprint).put("report_config_generation",Math.addExact(j.optLong("report_config_generation"),1))}
  store.document().optLong("report_config_generation")
 }
 fun snapshot(context:Context,state:MdmState):JSONObject=synchronized(MdmConfiguration.lock){
  check(state.active && !state.cleanupPending)
  val store=MdmStore(context)
  val result=JSONObject().put("version",1).put("name",("${android.os.Build.MANUFACTURER} ${android.os.Build.MODEL}").take(100))
   .put("appVersion",BuildConfig.VERSION_NAME).put("capabilities",JSONArray(listOf("web-control-v1")))
   .put("appliedRevision",state.appliedRevision)
  if(state.rights.vpn)result.put("vpnState",if(LabVpnService.active)"running" else "stopped")
  var generation=store.document().optLong("report_config_generation")
  if(state.rights.config){
   val document=MdmConfiguration.snapshotLocal(context)
   val fingerprint=hash(document.toString())
   store.edit{j->if(j.optString("report_config_hash")!=fingerprint){j.put("report_config_hash",fingerprint).put("report_config_generation",Math.addExact(j.optLong("report_config_generation"),1))};generation=j.optLong("report_config_generation")}
   result.put("configuration",document)
   val pm=context.packageManager
   val launchable=pm.queryIntentActivities(Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER),0).associate{it.activityInfo.packageName to it.loadLabel(pm).toString()}
   val entries=JSONArray()
   pm.getInstalledApplications(0).filter{it.packageName!=context.packageName && it.packageName.matches(Regex("[A-Za-z][A-Za-z0-9_]*(\\.[A-Za-z0-9_]+)+"))}
    .sortedBy{it.packageName}.take(2048).forEach{app->entries.put(JSONObject().put("packageId",app.packageName).put("label",(launchable[app.packageName]?:app.loadLabel(pm).toString()).take(256)))}
   // Android package visibility can restrict the returned set; never claim a complete inventory.
   val inventoryHash=hash(entries.toString())
   val inventory=JSONObject().put("hash",inventoryHash).put("complete",false)
   if(store.document().optString("inventory_ack")!=inventoryHash)inventory.put("entries",entries)
   result.put("inventory",inventory)
  }
  store.edit{j->j.put("report_sequence",Math.addExact(j.optLong("report_sequence"),1));result.put("sequence",j.getLong("report_sequence"))}
  result.put("configGeneration",generation)
 }
}
