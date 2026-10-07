package ru.vpnc.quiclab

import android.util.AtomicFile
import mobile.Mobile
import mobile.TrafficBudget
import java.io.File
import java.util.UUID

/** Main-thread owner. Reconnecting exits reuse the same durable budget period. */
internal class VpnBudgetRun(private val file:File?=null,private val bootCount:Int = -1) {
 var current:TrafficBudget?=null
  private set
 private var resumeEligible=false
 var restored=false
  private set
 companion object{
  fun forContext(c:android.content.Context)=VpnBudgetRun(File(c.filesDir,"vpn-budget.json"),android.provider.Settings.Global.getInt(c.contentResolver,android.provider.Settings.Global.BOOT_COUNT,-1))
 }
 fun shouldResume():Boolean {
  if(bootCount<0||file==null)return false
  return runCatching{AtomicFile(file).openRead().use{org.json.JSONObject(it.readBytes().toString(Charsets.UTF_8)).let{j->
   if(j.optInt("boot_count",-2)!=bootCount || !j.optBoolean("resume",false))false
   else {Mobile.restoreTrafficBudget(j.toString());true}
  }}}.getOrDefault(false)
 }
 fun start(limitBytes:Long):TrafficBudget {
  require(limitBytes>=0)
  current?.let{return it}
  val saved=file?.let{AtomicFile(it)}
  val raw=if(saved!=null && (file.exists()||File(file.path+".bak").exists()))saved.openRead().use{it.readBytes().toString(Charsets.UTF_8)}else null
  val meter=try {
   if(raw==null)Mobile.newTrafficBudget(UUID.randomUUID().toString(),limitBytes) else {
    val savedState=org.json.JSONObject(raw)
    // Validate before applying an explicitly changed configured limit.
    Mobile.restoreTrafficBudget(raw)
    if(savedState.getLong("limit")!=limitBytes){
     savedState.put("limit",limitBytes)
     savedState.put("blocked",limitBytes>0 && savedState.getLong("used")>=limitBytes)
    }
    Mobile.restoreTrafficBudget(savedState.toString())
   }
  }catch(e:Exception){throw IllegalStateException("Не удалось восстановить LTE-бюджет. В общих настройках можно явно начать новый период.",e)}
  current=meter;restored=raw!=null
  try{checkpoint()}catch(e:Exception){current=null;throw e}
  return meter
 }
 fun setResumeEligible(value:Boolean){resumeEligible=value;checkpoint()}
 fun checkpoint(){
  val raw=org.json.JSONObject(current?.snapshot() ?: return).put("boot_count",bootCount).put("resume",resumeEligible).toString()
  val target=file ?: return
  val atomic=AtomicFile(target)
  val out=atomic.startWrite()
  try{out.write(raw.toByteArray(Charsets.UTF_8));atomic.finishWrite(out)}
  catch(e:Exception){atomic.failWrite(out);throw e}
 }
 /** Service teardown is not explicit user Stop. Preserve accounting for restart. */
 fun release(){try{checkpoint()}finally{current=null}}
 fun stop(){resumeEligible=false;current=null;file?.let{AtomicFile(it).delete()};restored=false}
}
