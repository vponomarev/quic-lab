package ru.vpnc.quiclab

import android.app.NotificationManager
import android.app.job.JobInfo
import android.app.job.JobScheduler
import android.content.ComponentName
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

class MdmQuietJobTest {
 @Test fun realJobRunsWithoutNotificationAndSystemStopClosesSession(){
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext
  assumeTrue("Requires idle VPN and MDM",!LabVpnService.active && !MdmService.foregroundReady && !MdmStore(c).read().active)
  val scheduler=c.getSystemService(JobScheduler::class.java);val jobId=0x514d44fe
  check(scheduler.getPendingJob(jobId)==null)
  val started=CountDownLatch(1);val closed=CountDownLatch(1)
  val bound=CountDownLatch(1)
  val connection=object:android.content.ServiceConnection{
   override fun onServiceConnected(name:ComponentName,binder:android.os.IBinder){bound.countDown()}
   override fun onServiceDisconnected(name:ComponentName){}
  }
  // Keep the Service alive: onDestroy must not mask a broken onStopJob callback.
  val binding=c.bindService(android.content.Intent(c,MdmQuietJob::class.java),connection,android.content.Context.BIND_AUTO_CREATE)
  check(binding)
  val notices=c.getSystemService(NotificationManager::class.java).activeNotifications.map{it.id}.toSet()
  MdmQuietJob.owner={object:MdmSession{
   override fun sync(){started.countDown();Thread.sleep(30000)}
   override fun pause(){}
   override fun close(){closed.countDown()}
  }}
  fun shell(command:String)=inst.uiAutomation.executeShellCommand(command).use{p->android.os.ParcelFileDescriptor.AutoCloseInputStream(p).use{it.readBytes().toString(Charsets.UTF_8)}}
  try{
   assertTrue(bound.await(5,TimeUnit.SECONDS))
   val job=JobInfo.Builder(jobId,ComponentName(c,MdmQuietJob::class.java)).setMinimumLatency(1000).build()
   assertEquals(JobScheduler.RESULT_SUCCESS,scheduler.schedule(job))
   shell("cmd jobscheduler run -f ${c.packageName} $jobId")
   assertTrue("Scheduled job did not run",started.await(10,TimeUnit.SECONDS))
   assertFalse(MdmService.foregroundReady);assertFalse(MdmTelemetryService.running)
   assertEquals(notices,c.getSystemService(NotificationManager::class.java).activeNotifications.map{it.id}.toSet())
   shell("cmd jobscheduler timeout ${c.packageName} $jobId")
   assertTrue("System stop did not cancel session",closed.await(10,TimeUnit.SECONDS))
  }finally{
   scheduler.cancel(jobId);MdmQuietJob.cancel()
   val until=android.os.SystemClock.elapsedRealtime()+5000
   while(MdmQuietJob.busy() && android.os.SystemClock.elapsedRealtime()<until)Thread.sleep(20)
   MdmQuietJob.owner=null
   c.unbindService(connection)
  }
  assertFalse(MdmQuietJob.busy())
 }
}
