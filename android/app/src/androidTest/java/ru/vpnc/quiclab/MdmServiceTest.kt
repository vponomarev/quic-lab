package ru.vpnc.quiclab
import android.content.Context
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

class MdmServiceTest {
 @Test fun startsWithoutVpnAndPauseStopsControl(){
  val context=InstrumentationRegistry.getInstrumentation().targetContext
  val started=CountDownLatch(1);val paused=CountDownLatch(1)
  MdmService.owner={object:MdmSession{
   override fun sync(){started.countDown();Thread.sleep(100)}
   override fun pause(){paused.countDown()}
   override fun close(){}
  }}
  try{
   MdmService.start(context)
   assertTrue("FGS did not start",started.await(5,TimeUnit.SECONDS))
   context.startService(android.content.Intent(context,MdmService::class.java).setAction("pause"))
   assertTrue("Pause not delegated",paused.await(5,TimeUnit.SECONDS))
  }finally{MdmService.stop(context);MdmService.owner=null}
 }
}
