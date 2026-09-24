package ru.vpnc.quiclab
import android.os.ParcelFileDescriptor
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry
import androidx.test.runner.lifecycle.Stage
import org.junit.Test
import org.junit.Assert.*
class VpnLaunchTest {
 @Test fun launchAndWaitForUserConsent() {
  val args = InstrumentationRegistry.getArguments()
  org.junit.Assume.assumeTrue("Explicit device test only", args.getString("vpn_ui") == "true")
  val inst=InstrumentationRegistry.getInstrumentation()
  val prefs = inst.targetContext.getSharedPreferences("vpn", 0)
  val transport = args.getString("transport", "quic")!!
  require(transport == "quic" || transport == "https")
  prefs.edit().putString("transport", transport)
   .putString("endpoint", prefs.getString("${transport}_endpoint", "")).commit()
  VpnIdentity.load(inst.targetContext)
  inst.uiAutomation.executeShellCommand("am start -W -n ru.vpnc.quiclab/.MainActivity").use { ParcelFileDescriptor.AutoCloseInputStream(it).readBytes() }
  fun views(v:View):List<View> = listOf(v)+(if(v is ViewGroup)(0 until v.childCount).flatMap{views(v.getChildAt(it))}else emptyList())
  Thread.sleep(2500)
  inst.runOnMainSync {
   val a=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<MainActivity>().first()
   views(a.window.decorView).filterIsInstance<Button>().first{it.text=="VPN / Exit node"}.performClick()
  }
  Thread.sleep(2500)
  inst.runOnMainSync {
   val a=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<VpnActivity>().first()
   views(a.window.decorView).filterIsInstance<Button>().first{it.text=="Запустить VPN"}.performClick()
  }
  val deadline=System.currentTimeMillis()+90000
  while(System.currentTimeMillis()<deadline && (LabVpnService.connection=="—" || !LabVpnService.active))Thread.sleep(500)
  assertTrue("VPN consent and authenticated echo expected: ${LabVpnService.status}",LabVpnService.active && LabVpnService.connection!="—")
  if (args.getString("browser") == "true") {
   val hostname = prefs.getString("hostname", "")!!
   require(hostname.matches(Regex("[A-Za-z0-9.-]+")))
   inst.uiAutomation.executeShellCommand("am start -W -a android.intent.action.VIEW -d https://$hostname/vpn-demo/ -p com.android.chrome").use { ParcelFileDescriptor.AutoCloseInputStream(it).readBytes() }
   Thread.sleep(12000)
   assertTrue("VPN remains active during browser traffic", LabVpnService.active)
  }
 }
}
