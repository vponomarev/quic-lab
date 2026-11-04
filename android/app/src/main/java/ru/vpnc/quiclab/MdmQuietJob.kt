package ru.vpnc.quiclab

import android.app.job.JobParameters
import android.app.job.JobService
import android.os.Handler
import android.os.Looper
import java.util.concurrent.atomic.AtomicBoolean

/** No foreground service or long poll. JobScheduler owns the wake lock. */
class MdmQuietJob:JobService(){
 private val handler=Handler(Looper.getMainLooper())
 private var run:Run?=null
 private inner class Run(val params:JobParameters){
  val canceled=AtomicBoolean(false)
  @Volatile var systemStopped=false
  @Volatile var session:MdmSession?=null
  var worker:Thread?=null
  val timeout=Runnable{stop(false)}
  fun stop(system:Boolean){
   systemStopped=systemStopped||system
   canceled.set(true);session?.close();worker?.interrupt()
  }
  fun execute(){
   var retry=false
   try{
    val factory=owner
    if(canceled.get() || factory==null && MdmBackground.mode(this@MdmQuietJob)!="quiet")return
    val selected=if(factory!=null)factory(applicationContext) else MdmRuntime.session(applicationContext,quiet=true)
    if(selected==null){retry=true;return}
    session=selected
    if(canceled.get()){selected.close();return}
    MdmService.status="MDM · тихая синхронизация"
    selected.sync()
   }catch(_:Exception){MdmService.status="MDM · синхронизация отложена"}
   finally{
    session?.close()
    handler.post{
     handler.removeCallbacks(timeout)
     if(active===this)active=null
     if(run===this)run=null
     if(!systemStopped)jobFinished(params,retry && !canceled.get())
     if(passes.finish())runCatching{MdmBackground.configurationSubmitted(applicationContext)}
      .onFailure{MdmService.status="MDM · повторная синхронизация отложена"}
    }
   }
  }
 }
 override fun onStartJob(params:JobParameters):Boolean{
  if(owner==null && runCatching{MdmBackground.mode(this)!="quiet"}.getOrDefault(true))return false
  if(!passes.start())return false
  val next=Run(params);run=next;active=next
  handler.postDelayed(next.timeout,MAX_PASS_MS)
  next.worker=Thread({next.execute()},"mdm-quiet").also{it.start()}
  return true
 }
 override fun onStopJob(params:JobParameters):Boolean{run?.takeIf{it.params.jobId==params.jobId}?.stop(true);return false}
 override fun onDestroy(){run?.stop(true);super.onDestroy()}
 companion object{
  const val MAX_PASS_MS=90000L
  private val passes=MdmQuietPassGate()
  @Volatile private var active:Run?=null
  @Volatile internal var owner:((android.content.Context)->MdmSession?)?=null
  internal fun busy()=active!=null
  internal fun cancel(){passes.cancel();active?.stop(false)}
 }
}
