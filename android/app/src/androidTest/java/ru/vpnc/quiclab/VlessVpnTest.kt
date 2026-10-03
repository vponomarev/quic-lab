package ru.vpnc.quiclab

import android.content.Intent
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.VpnService
import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress
import java.net.URL
import javax.net.ssl.HttpsURLConnection
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Opt-in real standalone VPN test; private URI is removed even after failure. */
class VlessVpnTest {
 @Test fun standaloneTcpUdpAndNetworkChanges() {
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext
  assumeTrue(InstrumentationRegistry.getArguments().getString("vless_vpn_live")=="true")
  val fixture=File(c.filesDir,"vless-v1.txt")
  // Recover only this test's temporary fixture after a prior interrupted runner.
  val stale=VpnProfiles.list(c).filter{it.name=="VLESS · V1-test"}
  if(stale.isNotEmpty()) {VpnProfiles.select(c,VpnProfiles.list(c).first{it.id !in stale.map{p->p.id}}.id);stale.forEach{VpnProfiles.delete(c,it.id)}}
  val original=VpnProfiles.current(c).id;val before=VpnProfiles.list(c).map{it.id}.toSet()
  val multiple=VpnProfiles.multiple(c)
  val wifi=c.getSystemService(android.net.wifi.WifiManager::class.java).isWifiEnabled
  fun shell(cmd:String)=inst.uiAutomation.executeShellCommand(cmd).use{android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()}
  fun waitFor(label:String,timeout:Long=45000,check:()->Boolean){val end=SystemClock.elapsedRealtime()+timeout;while(SystemClock.elapsedRealtime()<end&&!check())Thread.sleep(200);assertTrue("$label · ${LabVpnService.status} · ${LabVpnService.network}",check())}
  fun probe(kind:String,host:String,port:Int):org.json.JSONObject {
   val pkg="ru.vpnc.quicprobe";val id="vless"+System.nanoTime()
   try {
    shell("am force-stop $pkg")
    shell("am start -W -n $pkg/ru.vpnc.quicprobe.ProbeActivity --es id $id --es kind $kind --es host $host --ei port $port")
    var text=""
    waitFor("Companion $kind",20000){text=shell("run-as $pkg cat files/$id.json").toString(Charsets.UTF_8);text.trim().startsWith("{")}
    val result=org.json.JSONObject(text)
    assertTrue("Companion must be captured by VPN",result.optBoolean("vpn"))
    assertTrue("Companion $kind failed: ${result.optString("error")}",result.optBoolean("ok"))
    return result
   } finally {shell("am force-stop $pkg");shell("run-as $pkg rm -f files/$id.json")}
  }
  fun tcpAndUdp(){
   val tcp=probe("https","api.ipify.org",443)
   assertTrue("IPv4 response expected",tcp.getString("reply").trim().matches(Regex("[0-9]+[.][0-9]+[.][0-9]+[.][0-9]+")))
   probe("dns","1.1.1.1",53)
  }
  try{
   assertFalse("Stop existing VPN before live acceptance",LabVpnService.active)
   assertNull("VPN consent required",VpnService.prepare(c))
   assertTrue("Private URI fixture required",fixture.isFile)
   VlessImport.save(c,fixture.readText());fixture.delete()
   VpnProfiles.setMultiple(c,false)
   VpnProfiles.preferences(c).edit().putInt("mode",1).putBoolean("global_apps",false).putStringSet("apps",setOf("ru.vpnc.quicprobe")).commit()
   c.startActivity(Intent(c,MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
   shell("svc wifi enable");Thread.sleep(2500)
   c.startForegroundService(Intent(c,LabVpnService::class.java))
   waitFor("VLESS Wi-Fi active"){LabVpnService.active&&LabVpnService.network.startsWith("Wi-Fi")}
   tcpAndUdp();println("VLESS standalone Wi-Fi TCP/UDP PASS")
   shell("svc wifi disable")
   waitFor("VLESS LTE active"){LabVpnService.active&&LabVpnService.network.startsWith("LTE")}
   tcpAndUdp();println("VLESS standalone LTE TCP/UDP PASS")
   shell("svc wifi enable")
   waitFor("VLESS Wi-Fi return"){LabVpnService.active&&LabVpnService.network.startsWith("Wi-Fi")}
   tcpAndUdp();println("VLESS standalone Wi-Fi return TCP/UDP PASS")
  }finally{
   fixture.delete();c.startService(Intent(c,LabVpnService::class.java).setAction("stop"));waitFor("VPN stopped"){!LabVpnService.active};Thread.sleep(1000)
   VpnProfiles.select(c,original);VpnProfiles.list(c).filter{it.id !in before}.forEach{VpnProfiles.delete(c,it.id)};VpnProfiles.setMultiple(c,multiple)
   shell(if(wifi)"svc wifi enable" else "svc wifi disable")
  }
 }
}
