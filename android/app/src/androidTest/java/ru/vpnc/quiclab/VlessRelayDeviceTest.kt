package ru.vpnc.quiclab

import android.content.Intent
import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test
import org.json.JSONObject

class VlessRelayDeviceTest {
 @Test fun actualTunKeepsTcpAfterCarrierFailure(){
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext
  val f=JSONObject(java.io.File(c.filesDir,"relay-fixture.json").readText())
  fun shell(cmd:String)=inst.uiAutomation.executeShellCommand(cmd).use {android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes().toString(Charsets.UTF_8)}
  fun waitFor(label:String,timeout:Long=45000,check:()->Boolean){val end=SystemClock.elapsedRealtime()+timeout;while(SystemClock.elapsedRealtime()<end && !check())Thread.sleep(200);assertTrue("$label: ${LabVpnService.status}; ${LabVpnService.bondSnapshot}",check())}
  fun ready():Int{val a=LabVpnService.bondSnapshot?.optJSONArray("paths")?:return 0;return (0 until a.length()).count{a.getJSONObject(it).optBoolean("ready")}}
  fun control(path:String,post:Boolean=false):JSONObject {
   val conn=java.net.URL(f.getString("control")+path).openConnection() as javax.net.ssl.HttpsURLConnection
   val ca=java.security.cert.CertificateFactory.getInstance("X.509").generateCertificate(f.getJSONObject("target").getString("ca").byteInputStream())
   val ks=java.security.KeyStore.getInstance(java.security.KeyStore.getDefaultType()).apply{load(null);setCertificateEntry("fixture",ca)}
   val trust=javax.net.ssl.TrustManagerFactory.getInstance(javax.net.ssl.TrustManagerFactory.getDefaultAlgorithm()).apply{init(ks)}
   conn.sslSocketFactory=javax.net.ssl.SSLContext.getInstance("TLS").apply{init(null,trust.trustManagers,null)}.socketFactory
   conn.hostnameVerifier=javax.net.ssl.HostnameVerifier {_,session->javax.net.ssl.HttpsURLConnection.getDefaultHostnameVerifier().verify("localhost",session)}
   conn.setRequestProperty("Authorization","Bearer "+f.getString("token"));conn.connectTimeout=5000;conn.readTimeout=5000
   if(post)conn.requestMethod="POST"
   return try{JSONObject(conn.inputStream.bufferedReader().use{it.readText()})}finally{conn.disconnect()}
  }
  val old=VpnProfiles.current(c).id;val multiple=VpnProfiles.multiple(c);val created=mutableListOf<String>()
  val probe="ru.vpnc.quicprobe";val probeId="relay"+System.nanoTime()
  try {
   assertFalse("MDM must be paused on the test device",MdmStore(c).read().active)
   assertNull("VPN consent required",android.net.VpnService.prepare(c))
   c.startService(Intent(c,LabVpnService::class.java).setAction("stop"));waitFor("stop"){!LabVpnService.active};Thread.sleep(500)
   VpnProfiles.setMultiple(c,false)
   val target=VpnProfiles.create(c,"Relay acceptance gateway");created.add(target.id)
   val identity=f.getJSONObject("target");VpnIdentity.writeBundle(c,target.id,identity)
   VpnProfiles.preferences(c,target.id).edit().putString("transport","https").putString("endpoint",identity.getString("endpoint")).putString("https_endpoint",identity.getString("endpoint")).putString("hostname","localhost").putString("ca",identity.getString("ca")).commit()
   val group=VpnProfiles.create(c,"Relay acceptance paths");created.add(group.id)
   VpnIdentity.writeBundle(c,group.id,f.getJSONObject("group"))
   val endpoint=JSONObject(f.getJSONObject("group").getString("vless_config")).getJSONObject("config").getString("Endpoint")
   VpnProfiles.preferences(c,group.id).edit().putString("transport","vless").putString("endpoint",endpoint).putString("relay_exit_profile_id",target.id).putInt("mode",1).putBoolean("global_apps",false).putStringSet("apps",setOf(probe)).putString("dns","1.1.1.1").commit()
   shell("input keyevent 224");shell("am start -W -n ru.vpnc.quiclab/.MainActivity");Thread.sleep(1500)
   c.startForegroundService(Intent(c,LabVpnService::class.java))
   waitFor("two data-verified paths",60000){ready()==2}
   val session=LabVpnService.connection
   shell("am start -W -n $probe/ru.vpnc.quicprobe.ProbeActivity --es id $probeId --es kind tcp-long --es host "+f.getString("probeHost")+" --ei port "+f.getInt("probePort"))
   waitFor("one destination accept"){control("/status").getInt("accepts")==1}
   Thread.sleep(3000);control("/stop/0",true)
   waitFor("surviving path"){ready()>=1};Thread.sleep(5000)
   assertEquals("Logical session must survive",session,LabVpnService.connection)
   var report=""
   waitFor("probe finished",65000){report=shell("run-as $probe cat files/$probeId.json");report.trim().startsWith("{")}
   val result=JSONObject(report);assertTrue("TCP report $result",result.optBoolean("ok"));assertTrue("Probe must use TUN",result.getBoolean("vpn"));assertEquals(1,result.getInt("connections"))
   assertTrue(result.getInt("exchanges")>20);assertTrue("warm failover pause",result.getLong("max_rtt_ms")<=3000)
   assertEquals("No hidden destination redial",1,control("/status").getInt("accepts"))
   shell("am force-stop $probe")
   val udpId=probeId+"udp"
   shell("am start -W -n $probe/ru.vpnc.quicprobe.ProbeActivity --es id $udpId --es kind udp --es host "+f.getString("probeHost")+" --ei port "+f.getInt("udpPort"))
   var udpReport=""
   waitFor("UDP after carrier failure",15000){udpReport=shell("run-as $probe cat files/$udpId.json");udpReport.trim().startsWith("{")}
   val udp=JSONObject(udpReport);assertTrue("UDP $udp",udp.getBoolean("ok"));assertTrue(udp.getBoolean("vpn"))
   shell("run-as $probe rm -f files/$udpId.json")
   println("RELAY DEVICE PASS UDP=true exchanges="+result.getInt("exchanges")+" max_rtt_ms="+result.getLong("max_rtt_ms")+" destination_accepts=1")
  }catch(e:Throwable){println("RELAY DEVICE FAILURE: "+e);throw e}finally{
   c.startService(Intent(c,LabVpnService::class.java).setAction("stop"));waitFor("cleanup stop"){!LabVpnService.active};Thread.sleep(1000)
   for(id in created.reversed())VpnProfiles.delete(c,id)
   VpnProfiles.select(c,old);VpnProfiles.setMultiple(c,multiple)
   shell("am force-stop $probe");shell("run-as $probe rm -f files/$probeId.json")
   java.io.File(c.filesDir,"relay-fixture.json").delete()
  }
 }
}
