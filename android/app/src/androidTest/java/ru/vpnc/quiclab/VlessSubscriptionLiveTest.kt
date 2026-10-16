package ru.vpnc.quiclab

import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.VpnService
import android.os.ParcelFileDescriptor
import androidx.test.platform.app.InstrumentationRegistry
import mobile.Mobile
import mobile.SocketBinder
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Opt-in live provider check. URL comes only from instrumentation arguments. */
class VlessSubscriptionLiveTest {
 @Test fun groupRotatesBlockedCandidate(){
  val args=InstrumentationRegistry.getArguments()
  assumeTrue(args.getString("subscription_group_live")=="true")
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  assertFalse("Stop VPN before probe",LabVpnService.active)
  val parsed=VlessSubscriptions.fetch(args.getString("subscription_url")?:error("Private URL required"))
  val original=VpnProfiles.current(c).id
  val groupSelection=parsed.copy(entries=parsed.entries.take(3))
  LocalConfigurationApply.apply(c,VlessSubscriptions.prepare(c,"Liberty group test",args.getString("subscription_url")!!,groupSelection,true))
  val count=VpnProfiles.list(c).size
  Mobile.validateMDMConfiguration(ConfigurationTransfer.snapshot(c,true).toString())
  LocalConfigurationApply.apply(c,VlessSubscriptions.prepare(c,"Liberty group test",args.getString("subscription_url")!!,groupSelection,true))
  assertEquals(count,VpnProfiles.list(c).size);assertEquals(original,VpnProfiles.current(c).id)
  println("GROUP_LIVE group_refresh_snapshot_current=PASS")
  val blocked="vless://11111111-1111-4111-8111-111111111111@192.0.2.1:443?security=tls&type=tcp&sni=example.com"
  val uris=org.json.JSONArray().put(blocked)
  parsed.entries.take(3).forEach{uris.put(it.uri)}
  val config=JSONObject().put("transport","vless").put("endpoint","192.0.2.1:443")
   .put("vless_config",Mobile.importVLESSConfig(blocked)).put("vless_uris",uris)
  val ready=java.util.concurrent.CountDownLatch(1)
  val healthy=java.util.concurrent.CountDownLatch(1)
  val switched=java.util.concurrent.atomic.AtomicBoolean(false)
  val cm=c.getSystemService(ConnectivityManager::class.java)
  val session=VpnSession(cm,null,config,-1,availability={_,kind,available->if(kind==VpnSession.WIFI && available)ready.countDown()},output={e->
   if(e.optString("event")=="vless_group"){
    println("GROUP_LIVE index=${e.optInt("index")} reason=${e.optString("reason")}")
    if(e.optString("reason")=="switched" && e.optInt("index")>1)switched.set(true)
    if(e.optString("reason")=="healthy" && e.optInt("index")>1)healthy.countDown()
   }
  },attached={})
  try{
   assertTrue("WiFi unavailable",ready.await(10,java.util.concurrent.TimeUnit.SECONDS))
   session.startOrMigrate(VpnSession.WIFI,"192.0.2.1:443","","",5000)
   assertTrue("No healthy candidate after failover",healthy.await(150,java.util.concurrent.TimeUnit.SECONDS))
   assertTrue(switched.get())
  }finally{session.close()}
 }
 @Test fun groupTunTcpUdp(){
  val args=InstrumentationRegistry.getArguments()
  assumeTrue(args.getString("subscription_group_tun")=="true")
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext
  assertFalse(LabVpnService.active);assertNull(VpnService.prepare(c))
  val parsed=VlessSubscriptions.fetch(args.getString("subscription_url")?:error("Private URL required"))
  val original=VpnProfiles.current(c).id;val multiple=VpnProfiles.multiple(c)
  val before=VpnProfiles.list(c).map{it.id}.toSet()
  fun shell(cmd:String)=inst.uiAutomation.executeShellCommand(cmd).use{ParcelFileDescriptor.AutoCloseInputStream(it).readBytes().toString(Charsets.UTF_8)}
  fun waitFor(label:String,timeout:Long,check:()->Boolean){val end=android.os.SystemClock.elapsedRealtime()+timeout;while(android.os.SystemClock.elapsedRealtime()<end&&!check())Thread.sleep(200);assertTrue(label,check())}
  fun probe(kind:String,host:String,port:Int){
   val pkg="ru.vpnc.quicprobe";val id="group"+System.nanoTime()
   try{
    shell("am force-stop $pkg")
    shell("am start -W -n $pkg/ru.vpnc.quicprobe.ProbeActivity --es id $id --es kind $kind --es host $host --ei port $port")
    var text=""
    waitFor("Companion response",25000){text=shell("run-as $pkg cat files/$id.json");text.trim().startsWith("{")}
    val result=JSONObject(text);assertTrue("Companion traffic outside VPN",result.optBoolean("vpn"));assertTrue("Companion "+kind+" failed",result.optBoolean("ok"))
   }finally{shell("am force-stop $pkg");shell("run-as $pkg rm -f files/$id.json")}
  }
  try{
   val blocked="vless://11111111-1111-4111-8111-111111111111@192.0.2.1:443?security=tls&type=tcp&sni=example.com#Group-TUN-test"
   VlessImport.save(c,blocked)
   val bundle=VpnIdentity.load(c);val uris=org.json.JSONArray().put(blocked);parsed.entries.take(3).forEach{uris.put(it.uri)}
   bundle.put("vless_uris",uris);VpnIdentity.writeBundle(c,VpnProfiles.current(c).id,bundle)
   VpnProfiles.setMultiple(c,false)
   VpnProfiles.preferences(c).edit().putInt("mode",1).putBoolean("global_apps",false).putStringSet("apps",setOf("ru.vpnc.quicprobe")).commit()
   c.startActivity(android.content.Intent(c,MainActivity::class.java).addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK))
   val started=System.currentTimeMillis()
   c.startForegroundService(android.content.Intent(c,LabVpnService::class.java))
   waitFor("Group not healthy after failover",150000){val events=VlessGroupEvents.snapshot();(0 until events.length()).any{val e=events.getJSONObject(it);e.optLong("time")>=started&&e.optString("reason")=="healthy"&&e.optInt("index")>1}}
   probe("https","api.ipify.org",443);probe("dns","1.1.1.1",53)
   println("GROUP_TUN rotation_https_dns=PASS")
  }finally{
   c.startService(android.content.Intent(c,LabVpnService::class.java).setAction("stop"));waitFor("VPN did not stop",10000){!LabVpnService.active};Thread.sleep(1000)
   VpnProfiles.select(c,original);VpnProfiles.list(c).filter{it.id !in before}.forEach{VpnProfiles.delete(c,it.id)};VpnProfiles.setMultiple(c,multiple)
  }
 }
 @Test fun restoreMDMAfterProbe(){
  assumeTrue(InstrumentationRegistry.getArguments().getString("restore_mdm")=="true")
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val controller=MdmRuntime.controller(c)
  if(!controller.read().active)controller.resume()
  MdmService.start(c)
  assertTrue(controller.read().active)
  println("SUB_LIVE mdm_active=true")
 }
 @Test fun fetchApplyRefreshAndHTTPS(){
  val args=InstrumentationRegistry.getArguments()
  assumeTrue(args.getString("subscription_live")=="true")
  val url=args.getString("subscription_url")?:error("Private URL required")
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val controller=MdmRuntime.controller(c)
  val active=controller.read().active
  val original=VpnProfiles.current(c).id
  try{
   if(active){controller.pause();MdmService.stop(c)}
   MdmConfiguration.assertEditable(c)
   if(args.getString("reset_test_profiles")=="true"){
    val group=java.util.UUID.nameUUIDFromBytes(url.toByteArray()).toString()
    for(key in VlessSubscriptions.selected(c,url)){
     val id=java.util.UUID.nameUUIDFromBytes(("vless-subscription:"+group+":"+key).toByteArray()).toString()
     if(id!=original && VpnProfiles.list(c).any{it.id==id})VpnProfiles.delete(c,id)
    }
   }
   val parsed=VlessSubscriptions.fetch(url)
   println("SUB_LIVE supported=${parsed.entries.size} skipped=${parsed.skipped}")
   val cm=c.getSystemService(ConnectivityManager::class.java)
   val wifi=cm.allNetworks.first{n->cm.getNetworkCapabilities(n)?.let{it.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)&&it.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)}==true}
   val protector=VpnService()
   val binder=object:SocketBinder{override fun bind(fd:Long){
    check(protector.protect(fd.toInt()))
    ParcelFileDescriptor.fromFd(fd.toInt()).use{wifi.bindSocket(it.fileDescriptor)}
   }}
   var working:VlessSubscriptions.Entry?=null
   for((i,e) in parsed.entries.take(5).withIndex()){
    val success=runCatching{
     val raw=JSONObject(Mobile.importVLESSConfig(e.uri)).getJSONObject("config")
     raw.put("Endpoint",resolveEndpoint(raw.getString("Endpoint"),wifi).address)
     val probe=Mobile.newVLESSProbe(raw.toString(),binder,null,"wifi")
     try{probe.httpsStatus("https://example.com/")==200L}finally{probe.close()}
    }.getOrDefault(false)
    println("SUB_LIVE endpoint_index=$i https_200=$success")
    if(success){working=e;break}
   }
   assertNotNull("No HTTPS success among first five supported endpoints",working)
   val selected=parsed.copy(entries=listOf(working!!))
   val before=VpnProfiles.list(c).size
   LocalConfigurationApply.apply(c,VlessSubscriptions.prepare(c,"Liberty",url,selected))
   val count=VpnProfiles.list(c).size
   assertTrue(count==before || count==before+1)
   assertEquals(original,VpnProfiles.current(c).id)
   assertTrue(working!!.key in VlessSubscriptions.selected(c,url))
   val refreshed=VlessSubscriptions.fetch(url)
   val chosen=refreshed.entries.filter{it.key==working!!.key}
   assertEquals(1,chosen.size)
   LocalConfigurationApply.apply(c,VlessSubscriptions.prepare(c,"Liberty",url,refreshed.copy(entries=chosen)))
   assertEquals(count,VpnProfiles.list(c).size)
   assertEquals(original,VpnProfiles.current(c).id)
   println("SUB_LIVE apply_refresh_no_duplicates_current_preserved=PASS")
  }finally{
   if(active){runCatching{controller.resume();MdmService.start(c)}.onFailure{println("SUB_LIVE mdm_resume_failed="+it.javaClass.simpleName)}}
   println("SUB_LIVE mdm_restored=${controller.read().active==active}")
  }
 }
}
