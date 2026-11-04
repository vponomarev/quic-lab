package ru.vpnc.quiclab

import android.app.job.JobInfo
import android.app.job.JobScheduler
import android.content.ComponentName
import android.content.Context
import android.os.SystemClock

internal interface MdmBackgroundPort {
 fun periodic();fun immediate();fun cancelJobs();fun cancelQuiet()
 fun startContinuous();fun stopContinuous();fun stopCollector()
}
internal object MdmBackground {

 fun configurationSubmitted(c:Context){
  // An explicit edit must not wait for the next two-hour quiet poll or the
  // foreground-open throttle. Provisioning still uses the existing job/session.
  if(mode(c)=="quiet"){
   check(c.getSystemService(JobScheduler::class.java).schedule(job(c,false))==JobScheduler.RESULT_SUCCESS)
  }else reconcile(c,true)
 }
 const val CAPABILITY="mdm_quiet_v1"
 const val SERVER_FLAG="server_mdm_quiet_v1"
 private const val PERIODIC=0x514d4401
 private const val IMMEDIATE=0x514d4402
 private var lastKick=Long.MIN_VALUE
 @Volatile var visible=false
 private fun prefs(c:Context)=c.getSharedPreferences("mdm-background",Context.MODE_PRIVATE)
 fun quiet(c:Context)=prefs(c).getBoolean("quiet",false)
 fun setQuiet(c:Context,value:Boolean){check(prefs(c).edit().putBoolean("quiet",value).commit())}
 fun mode(state:MdmState,quiet:Boolean,vpnActive:Boolean)=when {
  !state.active || state.cleanupPending || state.binding==null->"off"
  quiet && !vpnActive->"quiet"
  else->"continuous"
 }
 fun mode(c:Context)=mode(MdmStore(c).read(),quiet(c),LabVpnService.active)
 fun canSample(wanted:Boolean,fine:Boolean,background:Boolean,visible:Boolean)=wanted && fine && (background || visible)
 fun job(c:Context,periodic:Boolean):JobInfo {
  val builder=JobInfo.Builder(if(periodic)PERIODIC else IMMEDIATE,ComponentName(c,MdmQuietJob::class.java))
   .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
  if(periodic)builder.setPeriodic(2*60*60*1000L).setPersisted(true)
  else builder.setMinimumLatency(1000)
  return builder.build()
 }
 fun reconcile(mode:String,foreground:Boolean,port:MdmBackgroundPort){
  when(mode){
   "quiet"->{port.stopContinuous();port.stopCollector();port.periodic();if(foreground)port.immediate()}
   "continuous"->{port.cancelJobs();port.cancelQuiet();port.startContinuous()}
   else->{port.cancelJobs();port.cancelQuiet();port.stopContinuous();port.stopCollector()}
  }
 }
 fun reconcile(c:Context,foreground:Boolean=false){
  val scheduler=c.getSystemService(JobScheduler::class.java)
  val mode=mode(c)
  reconcile(mode,foreground,object:MdmBackgroundPort{
   override fun periodic(){if(scheduler.getPendingJob(PERIODIC)==null)check(scheduler.schedule(job(c,true))==JobScheduler.RESULT_SUCCESS)}
   override fun immediate(){synchronized(this@MdmBackground){
    val now=SystemClock.elapsedRealtime()
    if(MdmQuietJob.busy() || scheduler.getPendingJob(IMMEDIATE)!=null || lastKick!=Long.MIN_VALUE && now-lastKick<60000)return
    if(scheduler.schedule(job(c,false))==JobScheduler.RESULT_SUCCESS)lastKick=now
   }}
   override fun cancelJobs(){scheduler.cancel(PERIODIC);scheduler.cancel(IMMEDIATE)}
   override fun cancelQuiet(){MdmQuietJob.cancel()}
   override fun startContinuous(){MdmService.start(c)}
   override fun stopContinuous(){MdmService.stop(c)}
   override fun stopCollector(){MdmTelemetryService.stop(c)}
  })
  if(mode=="quiet")MdmService.status="MDM · тихий режим · ожидание синхронизации"
 }
}
