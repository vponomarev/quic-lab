package ru.vpnc.quiclab

import android.content.Intent
import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test
import org.json.JSONObject

class VlessProbeDeviceTest {
 @Test fun probesKeepWorkingTcpAndClassifyNodes(){
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
   assertEquals(0,control("/status").getInt("accepts"))
   val lease=VlessProbeNetwork.acquire(c)
   try {val native=mobile.Mobile.newVLESSProbe(JSONObject(f.getJSONObject("group").getString("vless_config")).getJSONObject("config").toString(),lease.binder,lease.budget,lease.kind)
    try {val result=JSONObject(native.run(JSONObject().put("target",f.getJSONObject("target").getString("control_url")+"/api/v1/probe/echo").put("ca",f.getJSONObject("target").getString("ca")).put("gateway",f.getJSONObject("target")).toString()));println("DIRECT FIXTURE PROBE: "+result);assertEquals("success",result.getString("outcome"))}finally{native.close()}
   }finally{lease.close()}
   VpnProfiles.setMultiple(c,false)
   val target=VpnProfiles.create(c,"Relay acceptance gateway");created.add(target.id)
   val identity=f.getJSONObject("target");VpnIdentity.writeBundle(c,target.id,identity)
   VpnProfiles.preferences(c,target.id).edit().putString("transport","https").putString("endpoint",identity.getString("endpoint")).putString("https_endpoint",identity.getString("endpoint")).putString("hostname","localhost").putString("ca",identity.getString("ca")).putString("control_url",identity.getString("control_url")).commit()
   val group=VpnProfiles.create(c,"Relay acceptance paths");created.add(group.id)
   VpnIdentity.writeBundle(c,group.id,f.getJSONObject("group"))
   val broken=java.net.URI(f.getJSONObject("group").getJSONArray("vless_uris").getString(0)).let{java.net.URI(it.scheme,it.userInfo,it.host,1,it.path,it.query,it.fragment).toASCIIString()}
   val bundle=VpnIdentity.load(c,group.id);bundle.getJSONArray("vless_uris").put(broken);VpnIdentity.writeBundle(c,group.id,bundle)
   val endpoint=JSONObject(f.getJSONObject("group").getString("vless_config")).getJSONObject("config").getString("Endpoint")
   VpnProfiles.preferences(c,group.id).edit().putString("transport","vless").putString("endpoint",endpoint).putString("relay_exit_profile_id",target.id).putInt("mode",1).putBoolean("global_apps",false).putStringSet("apps",setOf(probe)).putString("dns","1.1.1.1").commit()
   shell("input keyevent 224");shell("am start -W -n ru.vpnc.quiclab/.MainActivity");Thread.sleep(1500)
   c.startForegroundService(Intent(c,LabVpnService::class.java))
   waitFor("two data-verified paths",60000){ready()==2}
   val session=LabVpnService.connection
   shell("am start -W -n $probe/ru.vpnc.quicprobe.ProbeActivity --es id $probeId --es kind tcp-long --es host "+f.getString("probeHost")+" --ei port "+f.getInt("probePort"))
   waitFor("one destination accept"){control("/status").getInt("accepts")==1}
   VlessProbeCoordinator.foreground=true
   assertEquals("queued",VlessProbeCoordinator.start(c,group.id))
   waitFor("all probes finished",30000){VlessProbeCoordinator.results(c).optJSONObject(group.id)?.optString("state")=="finished"}
   val probeResult=VlessProbeCoordinator.results(c).getJSONObject(group.id);val rows=probeResult.getJSONArray("rows")
   assertEquals(3,rows.length());assertEquals("success",rows.getJSONObject(0).getString("outcome"));assertEquals("success",rows.getJSONObject(1).getString("outcome"));assertNotEquals("success",rows.getJSONObject(2).getString("outcome"))
   assertEquals(session,LabVpnService.connection)
   waitFor("local probe lease released"){!VlessProbeCoordinator.busy()}
   val temp=java.io.File(c.cacheDir,"probe-mdm-"+System.nanoTime()).apply{mkdirs()}
   java.io.File(c.noBackupFilesDir,"mdm-configuration.bin").takeIf{it.exists()}?.copyTo(java.io.File(temp,"mdm-configuration.bin"))
   val remote=object:android.content.ContextWrapper(c){override fun getNoBackupFilesDir()=temp}
   try {
    val store=MdmStore(remote);val rights=MdmRights(vpn=true,telemetry=true)
    store.edit{it.put("binding",MdmStore.bindingJson(MdmBinding("probe-fixture","https://quic-lab.vpnc.ru",1,rights,true))).put("active",true).put("rights",rights.json()).put("server_vless_probe_v1",true)}
    val now=java.time.Instant.now();val command=MdmCommand("remote-"+System.nanoTime(),"probe-fixture",1,now,now.plusSeconds(299),"vless_probe",JSONObject().put("sourceId",group.id).put("fingerprint",probeResult.getString("fingerprint")).put("action","start"))
    assertEquals("queued",MdmProbeControl.execute(remote,store.read(),command,now))
    assertEquals("queued",MdmProbeControl.execute(remote,store.read(),command,now))
    val cancel=command.copy(id=command.id+"-cancel",probe=JSONObject().put("action","cancel").put("runId",command.id))
    assertEquals("canceled",MdmProbeControl.execute(remote,store.read(),cancel,now))
    waitFor("remote canceled and drained"){!VlessProbeCoordinator.busy()}
    val out=JSONObject();MdmProbeControl.report(remote,store.read(),out);assertTrue(out.has("probeSnapshots"))
    assertFalse(out.getJSONArray("probeSnapshots").getJSONObject(0).has("ownerBinding"))
    store.edit{it.getJSONObject("binding").put("epoch",2)}
    val resumed=JSONObject();MdmProbeControl.report(remote,store.read(),resumed);assertFalse(resumed.has("probeSnapshots"))
    assertEquals(session,LabVpnService.connection)
   }finally{temp.deleteRecursively()}
   VlessProbeStore(c).save(probeResult)
   inst.runOnMainSync{c.startActivity(Intent(c,VlessProbeActivity::class.java).putExtra("source",group.id).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))}
   Thread.sleep(1000)
   println("PROBE DEVICE results="+rows.toString())
   control("/stop/0",true)
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
  }catch(e:Throwable){println("RELAY DEVICE FAILURE: "+e+"; "+VlessRelayStatus.snapshot());throw e}finally{
   c.startService(Intent(c,LabVpnService::class.java).setAction("stop"));waitFor("cleanup stop"){!LabVpnService.active};Thread.sleep(1000)
   for(id in created.reversed())VpnProfiles.delete(c,id)
   VpnProfiles.select(c,old);VpnProfiles.setMultiple(c,multiple)
   shell("am force-stop $probe");shell("run-as $probe rm -f files/$probeId.json")
   VlessProbeCoordinator.foreground=false
   java.io.File(c.filesDir,"relay-fixture.json").delete()
  }
 }
}
