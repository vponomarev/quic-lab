package ru.vpnc.quiclab

import android.util.AtomicFile
import mobile.Mobile
import mobile.TrafficBudget
import java.io.File
import java.util.UUID

/** Synchronized process-wide owner. Reconnecting exits reuse the same durable budget period. */
internal class VpnBudgetRun(private val file:File?=null,private val bootCount:Int = -1) {
 @Volatile var current:TrafficBudget?=null
  private set
 private var resumeEligible=false
 private var controls=0
 private var vpnOwned=false
 private var resetWhenIdle=false
 var restored=false
  private set
 companion object{
  private var shared:VpnBudgetRun?=null
  @Synchronized fun sharedForContext(c:android.content.Context):VpnBudgetRun = shared ?: forContext(c.applicationContext).also{shared=it}
  fun forContext(c:android.content.Context)=VpnBudgetRun(File(c.filesDir,"vpn-budget.json"),android.provider.Settings.Global.getInt(c.contentResolver,android.provider.Settings.Global.BOOT_COUNT,-1))
 }
 @Synchronized fun shouldResume():Boolean {
  if(bootCount<0||file==null)return false
  return runCatching{AtomicFile(file).openRead().use{org.json.JSONObject(it.readBytes().toString(Charsets.UTF_8)).let{j->
   if(j.optInt("boot_count",-2)!=bootCount || !j.optBoolean("resume",false))false
   else {Mobile.restoreTrafficBudget(j.toString());true}
  }}}.getOrDefault(false)
 }
 @Synchronized fun start(limitBytes:Long):TrafficBudget {
  val meter=startMeter(limitBytes);vpnOwned=true;resetWhenIdle=false;return meter
 }
 @Synchronized fun acquireControl(limitBytes:Long):TrafficBudget {
  val meter=startMeter(limitBytes);controls++;return meter
 }
 @Synchronized fun releaseControl(){
  check(controls>0);controls--;if(controls==0 && !vpnOwned){if(resetWhenIdle)stop() else release()}
 }
 private fun startMeter(limitBytes:Long):TrafficBudget {
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
 @Synchronized fun setResumeEligible(value:Boolean){resumeEligible=value;checkpoint()}
 @Synchronized fun checkpoint(){
  val raw=org.json.JSONObject(current?.snapshot() ?: return).put("boot_count",bootCount).put("resume",resumeEligible).toString()
  val target=file ?: return
  val atomic=AtomicFile(target)
  val out=atomic.startWrite()
  try{out.write(raw.toByteArray(Charsets.UTF_8));atomic.finishWrite(out)}
  catch(e:Exception){atomic.failWrite(out);throw e}
 }
 /** Service teardown is not explicit user Stop. Preserve accounting for restart. */
 @Synchronized fun release(){vpnOwned=false;try{checkpoint()}finally{if(controls==0)current=null}}
 @Synchronized fun stop(){
  resumeEligible=false;vpnOwned=false;resetWhenIdle=true
  if(controls>0){checkpoint();return}
  current=null;file?.let{AtomicFile(it).delete()};restored=false;resetWhenIdle=false
 }
}
