package ru.vpnc.quiclab

import android.content.Intent
import android.util.Base64
import androidx.test.platform.app.InstrumentationRegistry
import mobile.Mobile
import mobile.MDMResolver
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

class MdmTransportTest {
 private val ctx=InstrumentationRegistry.getInstrumentation().targetContext
 private fun fixture():Pair<MdmBinding,String>{
  val args=InstrumentationRegistry.getArguments()
  val endpoint=args.getString("mdm_endpoint").orEmpty()
  assumeTrue("Requires explicit Linux MDM HTTPS fixture",endpoint.isNotBlank())
  val ca=String(Base64.decode(args.getString("mdm_ca"),Base64.DEFAULT),Charsets.UTF_8)
  return MdmBinding("test-device",endpoint,1,MdmRights(),true) to ca
 }
 @Test fun controlWithoutVpn(){
  val (binding,ca)=fixture()
  assertFalse(LabVpnService.active)
  MdmTransport(ctx,{"test-secret"},ca).use {
   assertEquals(1,it.exchange(binding,MdmSyncRequest(binding.id,1,wait=false)).epoch)
  }
 }
 @Test fun controlSurvivesVpnStop(){
  val (binding,ca)=fixture()
  MdmTransport(ctx,{"test-secret"},ca).use {
   it.exchange(binding,MdmSyncRequest(binding.id,1,wait=false))
   ctx.stopService(Intent(ctx,LabVpnService::class.java))
   assertEquals(1,it.exchange(binding,MdmSyncRequest(binding.id,1,wait=false)).epoch)
  }
 }
 @Test fun redirectDoesNotLeakAuthorization(){
  val (binding,ca)=fixture()
  MdmTransport(ctx,{"redirect"},ca).use {
   assertTrue(runCatching{it.exchange(binding,MdmSyncRequest(binding.id,1,wait=false))}.isFailure)
  }
 }
 @Test fun cancelPollAndReplace(){
  val (binding,ca)=fixture()
  val t=MdmTransport(ctx,{"test-secret"},ca)
  val done=CountDownLatch(1)
  var succeeded=false
  val worker=Thread{try{t.exchange(binding,MdmSyncRequest(binding.id,1));succeeded=true}catch(_:Exception){}finally{done.countDown()}}
  worker.start();Thread.sleep(300);t.close()
  assertTrue("Poll did not cancel",done.await(2,TimeUnit.SECONDS));assertFalse(succeeded)
  MdmTransport(ctx,{"test-secret"},ca).use{
   assertEquals(1,it.exchange(binding,MdmSyncRequest(binding.id,1,wait=false)).epoch)
  }
 }
 @Test fun networkChangeCancelsPoll(){
  val args=InstrumentationRegistry.getArguments()
  assumeTrue("Requires dedicated phone network-switch opt-in",args.getString("mdm_network_loss")=="true")
  val (binding,ca)=fixture()
  val inst=InstrumentationRegistry.getInstrumentation()
  val cm=ctx.getSystemService(android.net.ConnectivityManager::class.java)
  val t=MdmTransport(ctx,{"test-secret"},ca)
  val done=CountDownLatch(1);var succeeded=false
  Thread{try{t.exchange(binding,MdmSyncRequest(binding.id,1));succeeded=true}catch(_:Exception){}finally{done.countDown()}}.start()
  try{
   Thread.sleep(800)
   inst.uiAutomation.executeShellCommand("svc wifi disable").close()
   assertTrue("Network loss left old poll alive",done.await(5,TimeUnit.SECONDS));assertFalse(succeeded)
  }finally{
   t.close();inst.uiAutomation.executeShellCommand("svc wifi enable").close()
  }
  val deadline=System.nanoTime()+TimeUnit.SECONDS.toNanos(25)
  while(System.nanoTime()<deadline){
   if(cm.allNetworks.any{cm.getNetworkCapabilities(it)?.let{c->c.hasTransport(android.net.NetworkCapabilities.TRANSPORT_WIFI)&&c.hasCapability(android.net.NetworkCapabilities.NET_CAPABILITY_VALIDATED)}==true})break
   Thread.sleep(300)
  }
  MdmTransport(ctx,{"test-secret"},ca).use{
   assertEquals(1,it.exchange(binding,MdmSyncRequest(binding.id,1,wait=false)).epoch)
  }
 }
 @Test fun budgetExceeded(){
  val b=Mobile.restoreTrafficBudget("""{"epoch":"blocked","limit":1,"used":1,"reserved":0,"blocked":true,"by_class":{"user":1}}""")
  var dnsCalls=0
  val c=Mobile.newMDMChannel(b,object:MDMResolver{
   override fun resolveAddress(host:String,port:Long):String{dnsCalls++;error("DNS must not run")}
  },"")
  try{
   assertTrue(runCatching{c.exchange("https://mdm.invalid/mdm/v1/sync","secret","{}",b.bind(null,"cell"))}.isFailure)
   assertEquals(0,dnsCalls)
  }finally{c.close()}
 }
}
