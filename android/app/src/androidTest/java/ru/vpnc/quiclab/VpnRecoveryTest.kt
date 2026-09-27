package ru.vpnc.quiclab
import android.content.Intent
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.io.File
class VpnRecoveryTest {
 @Test fun serverRestartRecoversAutomatically(){
  assumeTrue(InstrumentationRegistry.getArguments().getString("restart_live")=="true")
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val marker=File(c.filesDir,"recovery-test-state.txt");marker.delete()
  assertEquals("Use QUIC profile", "quic",VpnProfiles.preferences(c).getString("transport","quic"))
  fun waitFor(label:String,seconds:Int,ok:()->Boolean){val end=System.currentTimeMillis()+seconds*1000;while(!ok()&&System.currentTimeMillis()<end)Thread.sleep(100);assertTrue("$label: ${LabVpnService.status}",ok())}
  c.startActivity(Intent(c,MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));Thread.sleep(700)
  if(!LabVpnService.active)c.startForegroundService(Intent(c,LabVpnService::class.java))
  waitFor("initial connection",40){LabVpnService.lastEcho>0&&android.os.SystemClock.elapsedRealtime()-LabVpnService.lastEcho<1000}
  val before=LabVpnService.connection;val started=LabVpnService.startedAt
  marker.writeText("READY")
  waitFor("automatic recovery after external server restart",90){LabVpnService.connection!=before&&LabVpnService.lastEcho>0&&android.os.SystemClock.elapsedRealtime()-LabVpnService.lastEcho<1000}
  assertTrue(LabVpnService.active);assertEquals("VPN service must not restart",started,LabVpnService.startedAt)
  waitFor("transit recovered",15){LabVpnService.lastTransitEcho>0&&android.os.SystemClock.elapsedRealtime()-LabVpnService.lastTransitEcho<3000}
  marker.writeText("PASS")
 }
}
