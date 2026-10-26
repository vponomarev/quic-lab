package ru.vpnc.quiclab

import android.content.Intent
import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Test
import org.junit.Assert.*
import java.io.File

/** Explicit live acceptance only. Private fixture stays on-device and never enters the report. */
class FieldHaLiveTest {
 @Test fun ownServerAwgAndVlessAcrossRadios() {
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext
  val args=InstrumentationRegistry.getArguments()
  org.junit.Assume.assumeTrue("Explicit field_ha=true required",args.getString("field_ha")=="true")
  fun shell(cmd:String)=inst.uiAutomation.executeShellCommand(cmd).use{android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes().toString(Charsets.UTF_8)}
  if(args.getString("stage")=="consent") {
   android.net.VpnService.prepare(c)?.let{request->val activity=inst.startActivitySync(Intent(c,VpnActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK));inst.runOnMainSync{activity.startActivityForResult(request,999)};val end=SystemClock.elapsedRealtime()+90000;while(android.net.VpnService.prepare(c)!=null && SystemClock.elapsedRealtime()<end)Thread.sleep(200)}
   return
  }
  assertNull("VPN consent required",android.net.VpnService.prepare(c))
  assertFalse("Existing VPN must be stopped",LabVpnService.active)
  assertFalse("MDM must permit edits",MdmConfiguration.managed(c))
  val fixture=File(c.filesDir,"field-ha-private.json")
  val source=JSONObject(fixture.readText())
  if(args.getString("stage")=="refresh") {
   try { FieldSubscriptionDiagnostics.compareFresh(c) } finally { fixture.delete() }
   return
  }
  if(args.getString("stage")=="diagnose") {
   try { FieldSubscriptionDiagnostics.run(c,source,args.getString("fresh")=="true") } finally { fixture.delete() }
   return
  }
  val store=File(c.noBackupFilesDir,"mdm-configuration.bin")
  check(store.exists() && MdmConfiguration.hasLayer(c))
  val backup=File(c.noBackupFilesDir,"field-ha-original.bin")
  check(!backup.exists()){ "Unrestored prior field backup" }
  val original=store.readBytes();backup.writeBytes(original)
  val subscription=args.getString("vless_source")=="subscription"
  val subscriptionIdentity=if(subscription) {if(args.getString("fresh")=="true")FieldSubscriptionDiagnostics.freshIdentity(c) else FieldSubscriptionDiagnostics.identity(c)} else null
  val report=JSONObject().put("ok",false).put("vless_source",if(subscription)"subscription" else "own_server").put("fresh_subscription",subscription&&args.getString("fresh")=="true")
  val stages=org.json.JSONArray();report.put("stages",stages)
  val originalWifi=c.getSystemService(android.net.wifi.WifiManager::class.java).isWifiEnabled
  val originalData=shell("settings get global mobile_data").trim()
  val originalStay=shell("settings get global stay_on_while_plugged_in").trim()
  val originalCurrent=VpnProfiles.current(c).id;val originalMultiple=VpnProfiles.multiple(c)
  val originalReserve=VpnReserveSettings.preferences(c).all.toMap()
  fun waitFor(label:String,timeout:Long=60000,condition:()->Boolean){
   val end=SystemClock.elapsedRealtime()+timeout
   while(SystemClock.elapsedRealtime()<end && !condition())Thread.sleep(200)
   if(!condition())error(label+": "+LabVpnService.status+"; "+VlessRelayStatus.snapshot())
  }
  fun paths()=VlessRelayStatus.snapshot()?.optJSONArray("paths") ?: org.json.JSONArray()
  fun ready(network:String="",index:Int=0):Boolean {val p=paths();return (0 until p.length()).any{val a=p.getJSONObject(it);a.optBoolean("ready")&&(network.isBlank()||a.optString("network")==network)&&(index==0||a.optInt("index")==index)}}
  fun primary(network:String):Boolean {val p=paths();return (0 until p.length()).any{val a=p.getJSONObject(it);a.optBoolean("ready")&&a.optBoolean("primary")&&a.optString("network")==network}}
  fun checkpoint(name:String){stages.put(JSONObject().put("name",name).put("elapsed",SystemClock.elapsedRealtime()).put("connection",LabVpnService.connection).put("relay",VlessRelayStatus.snapshot()));File(c.filesDir,"field-ha-report.json").writeText(report.toString())}
  val probe="ru.vpnc.quicprobe"
  val probeIds=mutableListOf<String>()
  var pending:Throwable?=null
  try {
   shell("svc power stayon true");shell("input keyevent 224")
   check(originalWifi){"Start requires Wi-Fi enabled"}
   shell("svc data enable")
   VpnReserveSettings.preferences(c).edit().putBoolean("cell_on",true).putBoolean("cell_off",true).commit()
   VpnProfiles.setMultiple(c,false)
   val target=VpnProfiles.create(c,"Field HA own exit")
   VpnIdentity.writeBundle(c,target.id,JSONObject().put("certificate",source.getString("certificate")).put("key",source.getString("key")))
   VpnProfiles.preferences(c,target.id).edit().putString("transport","https").putString("endpoint",source.getString("https"))
    .putString("https_endpoint",source.getString("https")).putString("quic_endpoint",source.getString("quic"))
    .putString("hostname",source.getString("hostname")).putString("control_url",source.optString("control_url")).putString("ca",source.optString("ca")).commit()
   val vless=VpnProfiles.create(c,"Field HA VLESS")
   VpnIdentity.writeBundle(c,vless.id,subscriptionIdentity ?: JSONObject().put("vless_config",mobile.Mobile.importVLESSConfig(source.getString("vless_uri"))))
   VpnProfiles.preferences(c,vless.id).edit().putString("transport","vless").commit()
   val root=VpnProfiles.create(c,"Field HA AWG")
   VpnIdentity.writeBundle(c,root.id,JSONObject().put("awg_config",source.getString("awg_config")))
   VpnProfiles.preferences(c,root.id).edit().putString("transport","awg").putString("endpoint",AwgImport.metadata(source.getString("awg_config")).getString("endpoint"))
    .putString("dns","1.1.1.1").putInt("mode",1).putBoolean("global_apps",false).putStringSet("apps",setOf(probe))
    .putString("relay_exit_profile_id",target.id).putStringSet(AdaptiveGroups.MEMBERS,setOf(vless.id)).commit()
   shell("am start -W -n ru.vpnc.quiclab/.MainActivity")
   c.startForegroundService(Intent(c,LabVpnService::class.java))
   waitFor("AWG HA verified",120000){ready("wifi",1)&&primary("wifi")};checkpoint("awg_verified")
   waitFor("VLESS HA verified",120000){val p=paths();(0 until p.length()).any{p.getJSONObject(it).optBoolean("ready")&&p.getJSONObject(it).optInt("index")>1}};checkpoint("mixed_verified")
   waitFor("Warm LTE reserve",60000){ready("cell")&&primary("wifi")}
   val warmPaths=paths().toString()
   Thread.sleep(10000)
   assertTrue("LTE reserve disappeared before switching",ready("cell")&&primary("wifi"))
   report.put("warm_paths_initial",org.json.JSONArray(warmPaths));checkpoint("warm_lte_stable")
   report.put("warm_lte_before_switch",ready("cell"));checkpoint("before_switch")
   fun probeLeg(label:String,transition:()->Unit){
    val id=label+"_"+System.nanoTime();probeIds.add(id)
    val connection=LabVpnService.connection
    shell("am force-stop $probe")
    shell("am start -W -n $probe/ru.vpnc.quicprobe.ProbeActivity --es id $id --es kind https-long --es host "+source.getString("hostname")+" --ei port 443 --el duration_ms 90000")
    waitFor("First HTTPS exchange",20000){val raw=shell("run-as $probe cat files/$id.progress.json");raw.trim().startsWith("{") && JSONObject(raw).optInt("exchanges")>0}
    val transitionStart=SystemClock.elapsedRealtime();transition()
    check(SystemClock.elapsedRealtime()-transitionStart<75000){"Transition exceeded continuous probe duration"}
    var raw=""
    waitFor("Continuous HTTPS probe",110000){raw=shell("run-as $probe cat files/$id.json");raw.trim().startsWith("{")}
    val result=JSONObject(raw);stages.put(JSONObject().put("name",id).put("probe",result).put("same_session",connection==LabVpnService.connection))
    assertEquals(id,result.getString("id"));assertTrue("HTTPS probe failed: $result",result.optBoolean("ok"));assertTrue(result.getBoolean("vpn"));assertEquals(1,result.getInt("connections"));assertEquals(connection,LabVpnService.connection)
   }
   probeLeg("field_wifi_to_lte") {shell("svc wifi disable");waitFor("LTE after Wi-Fi loss"){primary("cell")};checkpoint("wifi_disabled")}
   probeLeg("field_lte_to_wifi") {shell("svc wifi enable");waitFor("Wi-Fi path restored",60000){ready("wifi")};checkpoint("wifi_restored");shell("svc data disable");waitFor("Wi-Fi after LTE loss"){primary("wifi")};checkpoint("data_disabled")}
   report.put("ok",true)
  }catch(t:Throwable){pending=t;report.put("error",t.message ?: t.javaClass.simpleName)}finally{
   val cleanup=org.json.JSONArray()
   fun attempt(label:String,action:()->Unit){try{action()}catch(t:Throwable){cleanup.put(label+":"+t.javaClass.simpleName)}}
   attempt("fixture_delete"){check(!fixture.exists()||fixture.delete())}
   attempt("stop"){c.startService(Intent(c,LabVpnService::class.java).setAction("stop"));waitFor("cleanup VPN stop",20000){!LabVpnService.active};Thread.sleep(1000)}
   attempt("probe_stop"){shell("am force-stop $probe")}
   attempt("wifi"){shell(if(originalWifi)"svc wifi enable" else "svc wifi disable");waitFor("restore Wi-Fi",15000){c.getSystemService(android.net.wifi.WifiManager::class.java).isWifiEnabled==originalWifi}}
   attempt("data"){if(originalData=="1")shell("svc data enable") else if(originalData=="0")shell("svc data disable");check(shell("settings get global mobile_data").trim()==originalData)}
   attempt("power"){if(originalStay=="null")shell("settings delete global stay_on_while_plugged_in") else shell("settings put global stay_on_while_plugged_in $originalStay")}
   attempt("configuration"){synchronized(MdmConfiguration.lock){val atomic=android.util.AtomicFile(store);val out=atomic.startWrite();try{out.write(original);atomic.finishWrite(out)}catch(t:Throwable){atomic.failWrite(out);throw t}}}
   val restored=runCatching{store.readBytes().contentEquals(original)&&VpnProfiles.current(c).id==originalCurrent&&VpnProfiles.multiple(c)==originalMultiple&&VpnReserveSettings.preferences(c).all==originalReserve}.getOrDefault(false)
   val wifiRestored=c.getSystemService(android.net.wifi.WifiManager::class.java).isWifiEnabled==originalWifi
   val dataRestored=runCatching{shell("settings get global mobile_data").trim()==originalData}.getOrDefault(false)
   report.put("configuration_restored",restored).put("wifi_restored",wifiRestored).put("mobile_data_restored",dataRestored).put("cleanup_errors",cleanup)
   if(restored)backup.delete()
   attempt("fixture_delete"){check(!fixture.exists()||fixture.delete())}
   for(id in probeIds)attempt("probe_result_delete"){shell("run-as $probe rm -f files/$id.json files/$id.progress.json")}
   if(!restored||!wifiRestored||!dataRestored||cleanup.length()>0)report.put("ok",false)
   File(c.filesDir,"field-ha-report.json").writeText(report.toString())
   assertTrue("Restoration failed: $cleanup",restored&&wifiRestored&&dataRestored&&cleanup.length()==0)
  }
  pending?.let{throw it}
 }
}
