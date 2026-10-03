package ru.vpnc.quiclab

import android.content.Intent
import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test

class BondDeviceTest {
 @Test fun pathsSurviveWifiLoss() {
  val inst=InstrumentationRegistry.getInstrumentation(); val c=inst.targetContext
  val p=VpnProfiles.preferences(c)
  val keys=listOf("transport","endpoint","max_availability","bond_copy_kib","bond_cell_mib","mode","global_apps","apps")
  val old=p.all.filterKeys{it in keys}
  val probe="ru.vpnc.quicprobe"; val probeId="bond"+System.nanoTime()
  val probeHost=InstrumentationRegistry.getArguments().getString("probe_host")?:error("probe_host required")
  val probeKind=InstrumentationRegistry.getArguments().getString("probe_kind")?:"https-long"
  require(probeKind in setOf("https-long","tcp-long"))
  val probePort=InstrumentationRegistry.getArguments().getString("probe_port")?.toInt()?:443
  require(probePort in 1..65535)
  require(probeHost.matches(Regex("[A-Za-z0-9.-]+")))
  c.packageManager.getApplicationInfo(probe,0)
  val wifi=c.getSystemService(android.net.wifi.WifiManager::class.java).isWifiEnabled
  fun shell(cmd:String)=inst.uiAutomation.executeShellCommand(cmd).use { android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes().toString(Charsets.UTF_8) }
  fun waitFor(label:String, timeout:Long=45000, check:()->Boolean) {
   val end=SystemClock.elapsedRealtime()+timeout
   while(SystemClock.elapsedRealtime()<end && !check()) Thread.sleep(200)
   assertTrue("$label: ${LabVpnService.status}; ${LabVpnService.bondSnapshot}",check())
  }
  fun ready(name:String):Boolean {
   val a=LabVpnService.bondSnapshot?.optJSONArray("paths")?:return false
   return (0 until a.length()).any {val v=a.getJSONObject(it);v.optString("network",v.optString("name"))==name && v.optBoolean("ready")}
  }
  val stay=shell("settings get global stay_on_while_plugged_in").trim()
  try {
   assertFalse("Select a single profile",VpnProfiles.multiple(c))
   assertNull("VPN consent required",android.net.VpnService.prepare(c))
   c.stopService(Intent(c,LabVpnService::class.java));waitFor("stop") {!LabVpnService.active};Thread.sleep(1000)
   val endpoint=p.getString("quic_endpoint",p.getString("endpoint",""))!!
   assertTrue("QUIC profile required",endpoint.isNotBlank())
   p.edit().putString("transport","quic").putString("endpoint",endpoint).putBoolean("max_availability",true).putLong("bond_copy_kib",256).putLong("bond_cell_mib",0).putInt("mode",1).putBoolean("global_apps",false).putStringSet("apps",setOf(probe)).commit()
   shell("svc power stayon true");shell("input keyevent 224");shell("svc wifi enable")
   shell("am start -W -n ru.vpnc.quiclab/.MainActivity")
   Thread.sleep(3000)
   c.startForegroundService(Intent(c,LabVpnService::class.java))
   waitFor("both paths") {ready("wifi") && ready("cell")}
   waitFor("session") {LabVpnService.connection.startsWith("bond-")}
   val session=LabVpnService.connection
   shell("am start -W -n $probe/ru.vpnc.quicprobe.ProbeActivity --es id $probeId --es kind $probeKind --es host $probeHost --ei port $probePort")
   Thread.sleep(3000)
   println("BOND both paths: ${LabVpnService.bondSnapshot}")
   shell("svc wifi disable")
   waitFor("LTE only") {ready("cell") && !ready("wifi")}
   Thread.sleep(4000);assertEquals("No logical restart after WiFi loss",session,LabVpnService.connection)
   println("BOND LTE: ${LabVpnService.bondSnapshot}")
   shell("svc wifi enable")
   waitFor("WiFi rejoined") {ready("wifi") && ready("cell")}
   Thread.sleep(10000);assertEquals("Same session after WiFi return",session,LabVpnService.connection)
   shell("svc power stayon false");shell("input keyevent 223");Thread.sleep(6000)
   assertTrue("WiFi stays active screen off",ready("wifi"));assertTrue("LTE stays active screen off",ready("cell"))
   assertEquals(session,LabVpnService.connection)
   var report=""
   waitFor("persistent TCP",60000) { report=shell("run-as $probe cat files/$probeId.json");report.trim().startsWith("{") }
   val result=org.json.JSONObject(report);println("BOND TCP $result")
   assertTrue("Persistent TCP: $result",result.optBoolean("ok"))
   assertEquals(1,result.optInt("connections"));assertTrue(result.optInt("exchanges")>20)
   println("BOND PASS screen off: ${LabVpnService.bondSnapshot}")
  } finally {
   shell("input keyevent 224");shell("settings put global stay_on_while_plugged_in $stay")
   c.stopService(Intent(c,LabVpnService::class.java));Thread.sleep(1500)
   p.edit().apply {keys.forEach{remove(it)};old.forEach{(k,v)->when(v){is String->putString(k,v);is Boolean->putBoolean(k,v);is Int->putInt(k,v);is Long->putLong(k,v);is Set<*>->putStringSet(k,v.filterIsInstance<String>().toSet())}}}.commit()
   shell("am force-stop $probe");shell("run-as $probe rm -f files/$probeId.json")
   shell(if(wifi)"svc wifi enable" else "svc wifi disable")
  }
 }
}
