package ru.vpnc.quiclab
import android.content.Intent
import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.io.File
/** Explicit opt-in test on an authorized device only; restores profiles and consent. */
class ResearchDirectLiveTest {
 @Test fun directAndTunnelScopeMatrix(){
  assumeTrue(InstrumentationRegistry.getArguments().getString("research_direct_live")=="true")
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext;val probe="ru.vpnc.quicprobe"
  val args=InstrumentationRegistry.getArguments();val echoHost=args.getString("research_echo_host")?:"192.168.5.214"
  // Initialize app networking before UiAutomation creates its system-package context.
  val appConnectivity=c.getSystemService(android.net.ConnectivityManager::class.java)
  fun restoreTestConnectivity(){
   if(android.os.Build.VERSION.SDK_INT==30)android.net.ConnectivityManager::class.java.getDeclaredField("sInstance").apply{isAccessible=true}.set(null,appConnectivity)
  }
  fun shell(cmd:String)=inst.uiAutomation.executeShellCommand(cmd).use{android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes().toString(Charsets.UTF_8)}
  fun waitFor(label:String,ms:Long=30000,check:()->Boolean){val end=SystemClock.elapsedRealtime()+ms;while(SystemClock.elapsedRealtime()<end&&!check())Thread.sleep(100);assertTrue(label+": "+LabVpnService.status,check())}
  // UiAutomation creates a system Context; refresh Android 11's static connectivity instance
  // with our package Context before VpnService.Builder.establish uses it.
  inst.uiAutomation
  c.createPackageContext(c.packageName,0).getSystemService(android.net.ConnectivityManager::class.java)
  val saved=File(c.filesDir,"research-original-mdm.private.bin");val state=if(saved.exists())MdmStore(saved).read()else MdmStore(c).read();val controller=MdmRuntime.controller(c);val wasRunning=LabVpnService.active
  var original="";var added="";var multiple=false;var leavePrepared=false;var createdAcceptanceBackup=false
  if(args.getString("research_leave_running")=="true")check(!File(c.noBackupFilesDir,"acceptance-research-state.bin").exists()){ "Restore the previous acceptance profile first" }
  fun consent(on:Boolean){if(state.binding!=null)controller.setRights(state.rights.copy(config=false,research=on,researchDirect=on))else {ResearchSettings.setStandalone(c,on);MdmStore(c).setStandaloneResearchDirect(on);ResearchSettings.changed()}}
  fun stop(){inst.runOnMainSync{LabVpnService.stopForMdm()};c.stopService(Intent(c,LabVpnService::class.java));waitFor("stop VPN"){!LabVpnService.active};Thread.sleep(700)}
  fun runProbe(kind:String,baseId:String,host:String=echoHost,port:Int=18456,pkg:String=probe){
   val id=baseId+"_"+System.currentTimeMillis().toString();shell("am force-stop $pkg");shell("run-as $pkg rm -f files/$id.json");shell("am start -n $pkg/ru.vpnc.quicprobe.ProbeActivity --es id $id --es kind $kind --es host $host --ei port $port")
   waitFor("probe $id",60000){shell("run-as $pkg cat files/$id.json").trim().startsWith("{")}
   val result=JSONObject(shell("run-as $pkg cat files/$id.json"));assertTrue(result.toString(),result.getBoolean("ok"));println("RESEARCH_PROBE "+result)
  }
  try {
   assertNull("VPN consent",android.net.VpnService.prepare(c));assertTrue("Active MDM binding required",state.binding==null||state.active)
   if(args.getString("research_leave_running")=="true") {
    val backup=File(c.noBackupFilesDir,"acceptance-research-state.bin");check(!backup.exists())
    val doc=MdmStore(c).document();MdmStore(backup).edit{j->doc.keys().forEach{k->j.put(k,doc.get(k))}}
    File(c.noBackupFilesDir,"acceptance-research-restore.json").writeText(JSONObject().put("profile",VpnProfiles.current(c).id).put("multiple",VpnProfiles.multiple(c)).put("running",wasRunning).toString())
    createdAcceptanceBackup=true
   }
   stop();consent(false);original=InstrumentationRegistry.getArguments().getString("original_profile")?:VpnProfiles.current(c).id;multiple=VpnProfiles.multiple(c);VpnProfiles.setMultiple(c,false);VpnProfiles.list(c).filter{it.name.startsWith("Research fixture")}.forEach{VpnProfiles.delete(c,it.id)}
   val profile=JSONObject(File(c.filesDir,"research-fixture.private.json").readText());ProfileImport.save(c,profile);added=VpnProfiles.current(c).id
   val bond=InstrumentationRegistry.getArguments().getString("research_bond")=="true"
   val p=VpnProfiles.preferences(c,added);p.edit().putBoolean("global_apps",false).putInt("mode",1).putStringSet("apps",setOf(probe)).putBoolean("demux_enabled",bond).putBoolean("max_availability",bond).putInt("quic_pool_size",3).putInt("https_pool_size",3).commit()
   if(args.getString("research_prepare_only")=="true"){
    check(createdAcceptanceBackup){"Preparation requires durable restoration backup"}
    val transport=args.getString("research_transport")?:"quic";require(transport in listOf("quic","https"))
    consent(true);MdmStore(c).setResearchScope(ResearchScope("all_apps",setOf("ru.vpnc.quicprobe.direct")));ResearchSettings.changed()
    VpnProfiles.preferences(c,added).edit().putString("transport",transport).putString("endpoint",profile.getString(transport)).commit()
    val budget=VpnBudgetRun.sharedForContext(c);budget.start(VpnBudgetSettings.limitBytes(c));budget.setResumeEligible(true);budget.release()
    leavePrepared=true;return
   }
   val requestedTransport=InstrumentationRegistry.getArguments().getString("research_transport")
   val transports=if(requestedTransport==null)listOf("quic","https")else listOf(requestedTransport.also{require(it in listOf("quic","https"))})
   for(transport in transports){
    for(mode in (InstrumentationRegistry.getArguments().getString("research_scope_mode")?.let{listOf(it)}?:listOf("vpn_only","vpn_plus_apps","all_apps"))){
     stop();consent(true)
     MdmStore(c).setResearchScope(ResearchScope(mode,setOf("ru.vpnc.quicprobe.direct")));ResearchSettings.changed()
     VpnProfiles.preferences(c,added).edit().putString("transport",transport).putString("endpoint",profile.getString(transport)).commit()
     shell("am start -n ru.vpnc.quiclab/.MainActivity");restoreTestConnectivity();c.startForegroundService(Intent(c,LabVpnService::class.java))
     waitFor("connected $transport/$mode",60000){LabVpnService.active&&LabVpnService.healthFresh(SystemClock.elapsedRealtime())}
     Thread.sleep(7000)
     println("RESEARCH_SCOPE_CASE $transport $mode profile=$added")
     runProbe("tcp","scope_${transport}_${mode}_vpn")
     runProbe("udp","scope_${transport}_${mode}_vpn_udp")
     runProbe("tcp","scope_${transport}_${mode}_direct",pkg="ru.vpnc.quicprobe.direct")
     runProbe("udp","scope_${transport}_${mode}_direct_udp",pkg="ru.vpnc.quicprobe.direct")
     runProbe("dns","scope_${transport}_${mode}_direct_dns","1.1.1.1",53,pkg="ru.vpnc.quicprobe.direct")
     if(args.getString("research_tls")=="true"){
      for(pkg in listOf(probe,"ru.vpnc.quicprobe.direct")){
       runProbe("tls-sni","tls_${transport}_${pkg}_sni".replace('.','_'),pkg=pkg)
       runProbe("tls-ech","tls_${transport}_${pkg}_ech".replace('.','_'),pkg=pkg)
      }
      runProbe("https","tls_${transport}_real_https","quic-lab.vpnc.ru",443)
     }
     Thread.sleep(if(InstrumentationRegistry.getArguments().getString("research_wait_window")=="true")65000 else 5000)
     if(args.getString("research_switch_networks")=="true") {
      for(wifi in listOf(false,true,false,true)) {
       shell("svc wifi "+if(wifi)"enable" else "disable")
       Thread.sleep(20000)
       waitFor("data path after network switch",60000){LabVpnService.healthFresh(SystemClock.elapsedRealtime())}
       val tag="handover_${transport}_${mode}_${if(wifi)"wifi" else "lte"}"
       runProbe("tcp",tag+"_vpn");runProbe("udp",tag+"_vpn_udp")
       runProbe("tcp",tag+"_direct",pkg="ru.vpnc.quicprobe.direct");runProbe("udp",tag+"_direct_udp",pkg="ru.vpnc.quicprobe.direct")
      }
     }
     val minutes=args.getString("research_soak_minutes")?.toIntOrNull()?:0
     repeat(minutes){minute->
      runProbe("tcp","soak_${transport}_${mode}_${minute}_vpn");runProbe("udp","soak_${transport}_${mode}_${minute}_vpn_udp")
      runProbe("tcp","soak_${transport}_${mode}_${minute}_direct",pkg="ru.vpnc.quicprobe.direct");runProbe("udp","soak_${transport}_${mode}_${minute}_direct_udp",pkg="ru.vpnc.quicprobe.direct")
      println("RESEARCH_SOAK_MEMORY "+shell("dumpsys meminfo ru.vpnc.quiclab | grep TOTAL"))
      Thread.sleep(60000)
     }
     if(args.getString("research_leave_running")=="true"){leavePrepared=true;return}
     val connection=LabVpnService.connection
     consent(false);Thread.sleep(1500)
     runProbe("tcp","scope_${transport}_${mode}_revoked",pkg="ru.vpnc.quicprobe.direct")
     assertTrue("VPN survives revoke",LabVpnService.active)
     assertEquals("No reconnect for consent",connection,LabVpnService.connection)
    }
   }

  }finally{
   if(!leavePrepared){
   shell("am force-stop $probe");shell("am force-stop ru.vpnc.quicprobe.direct");stop()
   if(added.isNotEmpty())VpnProfiles.delete(c,added)
   if(original.isNotEmpty())VpnProfiles.select(c,original)
   VpnProfiles.setMultiple(c,multiple)
   if(state.binding!=null)controller.setRights(state.rights)else {ResearchSettings.setStandalone(c,state.standaloneResearch);MdmStore(c).setStandaloneResearchDirect(state.standaloneResearchDirect)}
   MdmStore(c).setResearchScope(if(ResearchSettings.directAllowed(MdmStore(c).read()))state.researchScope else state.researchScope.vpnOnly());ResearchSettings.changed()
   if(wasRunning){shell("am start -n ru.vpnc.quiclab/.MainActivity");restoreTestConnectivity();c.startForegroundService(Intent(c,LabVpnService::class.java))}
   if(createdAcceptanceBackup){File(c.noBackupFilesDir,"acceptance-research-state.bin").delete();File(c.noBackupFilesDir,"acceptance-research-restore.json").delete()}
   }
  }
 }
 @Test fun restoreAcceptanceProfile(){
  assumeTrue(InstrumentationRegistry.getArguments().getString("research_restore")=="true")
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext
  val saved=File(c.noBackupFilesDir,"acceptance-research-state.bin");check(saved.exists())
  val state=MdmStore(saved).read();val meta=File(c.noBackupFilesDir,"acceptance-research-restore.json");val j=JSONObject(meta.readText())
  inst.runOnMainSync{LabVpnService.stopForMdm()};Thread.sleep(1000)
  VpnProfiles.list(c).filter{it.name.startsWith("Research fixture")}.forEach{VpnProfiles.delete(c,it.id)}
  VpnProfiles.select(c,j.getString("profile"));VpnProfiles.setMultiple(c,j.getBoolean("multiple"))
  if(state.binding!=null)MdmRuntime.controller(c).setRights(state.rights)else{ResearchSettings.setStandalone(c,state.standaloneResearch);MdmStore(c).setStandaloneResearchDirect(state.standaloneResearchDirect)}
  MdmStore(c).setResearchScope(if(ResearchSettings.directAllowed(MdmStore(c).read()))state.researchScope else state.researchScope.vpnOnly());ResearchSettings.changed()
  saved.delete();meta.delete()
  if(j.getBoolean("running") || InstrumentationRegistry.getArguments().getString("research_restore_start")=="true") {
   // Instrumentation exit stops this process. Prepare the authorized restoration;
   // the harness launches MainActivity normally and verifies the actual service afterward.
   val budget=VpnBudgetRun.sharedForContext(c)
   budget.start(VpnBudgetSettings.limitBytes(c));budget.setResumeEligible(true);budget.release()
   assertTrue("Original VPN prepared for normal launcher restoration",budget.shouldResume())
  }
 }
}
