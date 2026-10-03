package ru.vpnc.quiclab

import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.VpnService
import android.os.ParcelFileDescriptor
import androidx.test.platform.app.InstrumentationRegistry
import mobile.Mobile
import mobile.SocketBinder
import mobile.VLESSProbe
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

class VlessEngineTest {
 @Test fun twoPhysicalNetworksUseIndependentEngines() {
  val inst=InstrumentationRegistry.getInstrumentation()
  val args=InstrumentationRegistry.getArguments()
  assumeTrue(args.getString("vless_live")=="true")
  val c=inst.targetContext
  assertFalse("Stop the current VPN before this diagnostic",LabVpnService.active)
  assertNull("VPN permission required",VpnService.prepare(c))
  val cm=c.getSystemService(ConnectivityManager::class.java)
  val wifiManager=c.getSystemService(android.net.wifi.WifiManager::class.java)
  assertTrue("Wi-Fi must initially be enabled",wifiManager.isWifiEnabled)
  val config=JSONObject(File(c.filesDir,"vless-v0.json").readText())
  val target=args.getString("vless_target")?:"https://quic-demo.vpnc.ru/"
  val expectedStatus=args.getString("vless_status")?.toInt()?:302 // Verified redirect from this fixture endpoint.
  val ready=CountDownLatch(1)
  val callback=object:ConnectivityManager.NetworkCallback(){override fun onAvailable(network:Network){ready.countDown()}}
  val pool=Executors.newFixedThreadPool(2)
  val probes=mutableListOf<VLESSProbe>()
  var wifiChanged=false
  fun shell(cmd:String)=inst.uiAutomation.executeShellCommand(cmd).use{ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()}
  fun selected(kind:Int):Network=cm.allNetworks.firstOrNull{n->cm.getNetworkCapabilities(n)?.let{it.hasTransport(kind)&&it.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)&&it.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)}==true}?:error("Required physical network unavailable")
  cm.requestNetwork(NetworkRequest.Builder().addTransportType(NetworkCapabilities.TRANSPORT_CELLULAR).addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET).build(),callback)
  try {
   assertTrue("Cellular network unavailable",ready.await(30,TimeUnit.SECONDS))
   val wifi=selected(NetworkCapabilities.TRANSPORT_WIFI)
   val cell=selected(NetworkCapabilities.TRANSPORT_CELLULAR)
   val budget=Mobile.newTrafficBudget("vless-v0",1024*1024)
   val binds=mapOf("wifi" to AtomicInteger(),"cell" to AtomicInteger())
   val protector=VpnService()
   for((name,network) in listOf("wifi" to wifi,"cell" to cell)) {
    val resolved=resolveEndpoint(config.getString("Endpoint"),network)
    val raw=JSONObject(config.toString()).put("Endpoint",resolved.address)
    val binder=object:SocketBinder{override fun bind(fd:Long){
     check(protector.protect(fd.toInt())){"VLESS socket protection failed"}
     ParcelFileDescriptor.fromFd(fd.toInt()).use{network.bindSocket(it.fileDescriptor)}
     binds.getValue(name).incrementAndGet()
    }}
    probes+=Mobile.newVLESSProbe(raw.toString(),binder,budget,name)
   }
   val results=probes.map{probe->pool.submit<Long>{probe.httpsStatus(target)}}
   results.forEach{assertEquals(expectedStatus,it.get(30,TimeUnit.SECONDS).toInt())}
   assertTrue("Wi-Fi binder unused",binds.getValue("wifi").get()>0)
   assertTrue("Cellular binder unused",binds.getValue("cell").get()>0)
   wifiChanged=true;shell("svc wifi disable")
   val until=System.nanoTime()+TimeUnit.SECONDS.toNanos(15)
   while(cm.getNetworkCapabilities(wifi)!=null&&System.nanoTime()<until)Thread.sleep(100)
   assertNull("Wi-Fi remained available",cm.getNetworkCapabilities(wifi))
   assertEquals("Cellular engine lost its network binding",expectedStatus.toLong(),probes[1].httpsStatus(target))
   assertTrue("Unexpected diagnostic traffic volume",JSONObject(budget.snapshot()).getLong("used")<1024*1024)
   println("VLESS_V0: two engines passed HTTPS; cellular stayed usable after Wi-Fi removal; shared budget below 1 MiB")
  } finally {
   probes.forEach{it.close()};pool.shutdownNow();cm.unregisterNetworkCallback(callback)
   if(wifiChanged){shell("svc wifi enable")}
   File(c.filesDir,"vless-v0.json").delete()
  }
 }
}