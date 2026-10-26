package ru.vpnc.quiclab

import android.net.NetworkCapabilities
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Physical binding smoke tests: acquire/release only, never open a socket or change settings. */
class VlessProbeNetworkTest {
 private val context get()=InstrumentationRegistry.getInstrumentation().targetContext
 @Test fun currentPhysicalNetworkCanBeLeasedAndClosedTwiceWithoutPayload() {
  val kind=VlessProbeNetwork.selectedKind(context)
  assumeTrue("No physical network available",kind!=null)
  val lease=acquireOrSkip(kind!!)
  try {
   assertEquals(kind,lease.kind)
   assertTrue(VlessProbeNetwork.available(context,lease.network,kind))
  } finally {lease.close();lease.close()}
 }
 @Test fun permittedSecondaryCellCanBeRetainedWithoutPayload() {
  assumeTrue("Secondary LTE requires primary Wi-Fi",VlessProbeNetwork.selectedKind(context)=="wifi")
  assumeTrue("LTE reserve disabled by user",VpnReserveSettings.allowed(context,NetworkCapabilities.TRANSPORT_CELLULAR,false))
  val lease=acquireOrSkip("cell")
  try {
   assertEquals("cell",lease.kind)
   assertTrue(VlessProbeNetwork.available(context,lease.network,"cell"))
  } finally {lease.close()}
 }
 private fun acquireOrSkip(kind:String):ProbeNetworkLease {
  try{return VlessProbeNetwork.acquire(context,kind)}catch(e:IllegalStateException){
   if(e.message in listOf("no_network","budget"))assumeTrue("Physical test unavailable: ${e.message}",false)
   throw e
  }
 }
}
