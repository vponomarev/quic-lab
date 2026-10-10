package ru.vpnc.quiclab
import android.content.Intent
import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.io.File
/** Explicit opt-in test on the authorized Note9 only; restores profiles and consent. */
class ResearchLiveTest {
 @Test fun ownTunnelFlowAttributionAndLiveConsent(){
  assumeTrue(InstrumentationRegistry.getArguments().getString("research_live")=="true")
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext;val probe="ru.vpnc.quicprobe"
  // Initialize app networking before UiAutomation creates its system-package context.
  c.getSystemService(android.net.ConnectivityManager::class.java)
  fun shell(cmd:String)=inst.uiAutomation.executeShellCommand(cmd).use{android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes().toString(Charsets.UTF_8)}
  fun waitFor(label:String,ms:Long=30000,check:()->Boolean){val end=SystemClock.elapsedRealtime()+ms;while(SystemClock.elapsedRealtime()<end&&!check())Thread.sleep(100);assertTrue(label+": "+LabVpnService.status,check())}
  val saved=File(c.filesDir,"research-original-mdm.private.bin");val state=if(saved.exists())MdmStore(saved).read()else MdmStore(c).read();val controller=MdmRuntime.controller(c);val wasRunning=LabVpnService.active
  var original="";var added="";var multiple=false
  fun consent(on:Boolean){if(state.binding!=null)controller.setRights(state.rights.copy(config=false,research=on))else ResearchSettings.setStandalone(c,on)}
  fun stop(){inst.runOnMainSync{LabVpnService.stopForMdm()};c.stopService(Intent(c,LabVpnService::class.java));waitFor("stop VPN"){!LabVpnService.active};Thread.sleep(700)}
  fun runProbe(kind:String,baseId:String,host:String="192.168.5.214",port:Int=18456,pkg:String=probe){
   val id=baseId+"_"+System.currentTimeMillis().toString();shell("am force-stop $pkg");shell("run-as $pkg rm -f files/$id.json");shell("am start -W -n $pkg/ru.vpnc.quicprobe.ProbeActivity --es id $id --es kind $kind --es host $host --ei port $port")
   waitFor("probe $id",60000){shell("run-as $pkg cat files/$id.json").trim().startsWith("{")}
   val result=JSONObject(shell("run-as $pkg cat files/$id.json"));assertTrue(result.toString(),result.getBoolean("ok"));assertEquals(result.toString(),pkg==probe,result.getBoolean("vpn"));println("RESEARCH_PROBE "+result)
  }
  try {
   assertNull("VPN consent",android.net.VpnService.prepare(c));assertTrue("Active MDM binding required",state.binding==null||state.active)
   stop();consent(false);original=InstrumentationRegistry.getArguments().getString("original_profile")?:VpnProfiles.current(c).id;multiple=VpnProfiles.multiple(c);VpnProfiles.setMultiple(c,false);VpnProfiles.list(c).filter{it.name.startsWith("Research fixture")}.forEach{VpnProfiles.delete(c,it.id)}
   val profile=JSONObject(File(c.filesDir,"research-fixture.private.json").readText());ProfileImport.save(c,profile);added=VpnProfiles.current(c).id
   val p=VpnProfiles.preferences(c,added);p.edit().putBoolean("global_apps",false).putInt("mode",1).putStringSet("apps",setOf(probe)).putBoolean("demux_enabled",false).commit()
   val requestedTransport=InstrumentationRegistry.getArguments().getString("research_transport")
   val transports=if(requestedTransport==null)listOf("quic","https")else listOf(requestedTransport.also{require(it in listOf("quic","https"))})
   for(transport in transports){
    stop();consent(false);VpnProfiles.preferences(c,added).edit().putString("transport",transport).putString("endpoint",profile.getString(transport)).commit()
    shell("am start -W -n ru.vpnc.quiclab/.MainActivity");c.startForegroundService(Intent(c,LabVpnService::class.java))
    waitFor("connected $transport",60000){LabVpnService.active&&LabVpnService.healthFresh(SystemClock.elapsedRealtime())}
    runProbe("tcp","research_${transport}_off");val connection=LabVpnService.connection
    consent(true);Thread.sleep(7000)
    runProbe("tcp-long","research_${transport}_long");runProbe("udp","research_${transport}_udp");runProbe("dns","research_${transport}_dns","1.1.1.1",53)
    runProbe("tcp","research_${transport}_direct",pkg="ru.vpnc.quicprobe.direct")
    Thread.sleep(65000);consent(false);Thread.sleep(1500);runProbe("tcp","research_${transport}_revoked")
    assertTrue("VPN survives revoke",LabVpnService.active);assertEquals("No reconnect for consent",connection,LabVpnService.connection);Thread.sleep(6000)
   }
  }finally{
   shell("am force-stop $probe");shell("am force-stop ru.vpnc.quicprobe.direct");stop()
   if(added.isNotEmpty())VpnProfiles.delete(c,added)
   if(original.isNotEmpty())VpnProfiles.select(c,original)
   VpnProfiles.setMultiple(c,multiple)
   if(state.binding!=null)controller.setRights(state.rights)else ResearchSettings.setStandalone(c,state.standaloneResearch)
   if(wasRunning){shell("am start -W -n ru.vpnc.quiclab/.MainActivity");c.startForegroundService(Intent(c,LabVpnService::class.java))}
  }
 }
}
