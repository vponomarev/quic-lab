package ru.vpnc.quiclab
import android.os.ParcelFileDescriptor
import android.view.View
import android.view.ViewGroup
import android.widget.*
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry
import androidx.test.runner.lifecycle.Stage
import org.junit.Test
import org.junit.Assert.*
class VpnDashboardTest {
 @Test fun startMetricsAndStopFromHome() {
  val args=InstrumentationRegistry.getArguments()
  org.junit.Assume.assumeTrue(args.getString("vpn_ui")=="true")
  val inst=InstrumentationRegistry.getInstrumentation()
  inst.uiAutomation.executeShellCommand("am start -W -n ru.vpnc.quiclab/.MainActivity").use{ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()}
  fun views(v:View):List<View> = listOf(v)+(if(v is ViewGroup)(0 until v.childCount).flatMap{views(v.getChildAt(it))}else emptyList())
  fun click(name:String) { inst.runOnMainSync {
   val a=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<MainActivity>().first()
   views(a.window.decorView).filterIsInstance<Button>().first{it.text==name}.performClick()
  } }
  Thread.sleep(2500)
  repeat(args.getString("cycles","1")!!.toInt()) { cycle ->
  val prefs=VpnProfiles.preferences(inst.targetContext)
  val transport=if(cycle%2==0) "quic" else "https"
  prefs.edit().putString("transport",transport).putString("endpoint",prefs.getString("${transport}_endpoint","")).commit()
  click("Start VPN")
  val until=System.currentTimeMillis()+30000
  while(System.currentTimeMillis()<until && (LabVpnService.lastEcho==0L || !LabVpnService.active)) Thread.sleep(200)
  assertTrue("VPN started from home",LabVpnService.active && LabVpnService.lastEcho>0)
  Thread.sleep(2500)
  inst.runOnMainSync {
   val a=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<MainActivity>().first()
   val text=views(a.window.decorView).filterIsInstance<TextView>().filter{it.isShown}.joinToString("\n"){it.text}
   assertTrue(text.contains("TX ↑")); assertTrue(text.contains("RTT:")); assertTrue(text.contains("Stop VPN"))
  }
  click("Stop VPN")
  Thread.sleep(1200)
  assertFalse(LabVpnService.active)
  assertEquals(0.0,LabVpnService.txRate,0.001)
  }
 }
}
