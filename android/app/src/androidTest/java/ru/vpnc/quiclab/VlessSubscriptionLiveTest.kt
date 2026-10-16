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
