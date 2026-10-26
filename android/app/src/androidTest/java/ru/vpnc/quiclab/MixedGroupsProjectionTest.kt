package ru.vpnc.quiclab

import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File
import java.util.UUID

class MixedGroupsProjectionTest {
 private val awgID="11111111-1111-4111-8111-111111111111"
 private val targetID="22222222-2222-4222-8222-222222222222"
 private val directID="33333333-3333-4333-8333-333333333333"
 private class Isolated(base:Context):ContextWrapper(base){
  val prefix="mixed-${UUID.randomUUID()}";val dir=File(base.cacheDir,prefix).apply{check(mkdirs())};val names=mutableSetOf<String>()
  override fun getFilesDir()=dir
  override fun getNoBackupFilesDir()=dir
  override fun getSharedPreferences(name:String,mode:Int):SharedPreferences {names.add(prefix+name);return baseContext.getSharedPreferences(prefix+name,mode)}
  fun clean(){names.forEach{baseContext.deleteSharedPreferences(it)};check(dir.parentFile==baseContext.cacheDir);dir.deleteRecursively()}
 }
 private fun awg():String {
  val key=android.util.Base64.encodeToString(ByteArray(32){(it+1).toByte()},android.util.Base64.NO_WRAP)
  return "[Interface]\nPrivateKey = $key\nAddress = 10.20.0.2\nDNS = 9.9.9.9\n[Peer]\nPublicKey = $key\nAllowedIPs = 0.0.0.0/0\nEndpoint = awg.example:52000\n"
 }
 private fun vless()=JSONObject().put("vless_config",mobile.Mobile.importVLESSConfig("vless://11111111-1111-4111-8111-111111111111@proxy.example:443?security=tls&type=tcp&sni=cover.example"))
 private fun own()=JSONObject().put("certificate","test-cert").put("key","test-key")
 private fun fixture(block:(Isolated)->Unit){
  val c=Isolated(InstrumentationRegistry.getInstrumentation().targetContext)
  try{
   check(!MdmConfiguration.hasLayer(c)&&c.noBackupFilesDir!=c.baseContext.noBackupFilesDir)
   val profiles=JSONArray();for(id in listOf("default",awgID,targetID,directID))profiles.put(JSONObject().put("id",id).put("name",id))
   c.getSharedPreferences("vpn_profiles",0).edit().putString("profiles",profiles.toString()).putString("current","default").commit()
   c.getSharedPreferences("vpn",0).edit().putString("transport","vless").putString("endpoint","proxy.example:443").putString("dns","10.10.0.1").putStringSet(AdaptiveGroups.MEMBERS,setOf(awgID)).commit()
   c.getSharedPreferences("vpn_$awgID",0).edit().putString("transport","awg").putString("endpoint","awg.example:52000").putString("dns","9.9.9.9").commit()
   for(id in listOf(targetID,directID)){
    c.getSharedPreferences("vpn_$id",0).edit().putString("transport","https").putString("endpoint","mux.example:443").putString("https_endpoint","mux.example:443").commit()
    VpnIdentity.writeBundle(c,id,own())
   }
   VpnIdentity.writeBundle(c,"default",vless());VpnIdentity.writeBundle(c,awgID,JSONObject().put("awg_config",awg()))
   block(c)
  }finally{c.clean()}
 }
 @Test fun normalMixedCandidatesUseSeparateCredentialsAndRootDns()=fixture{c->
  c.getSharedPreferences("vpn",0).edit().putStringSet(AdaptiveGroups.MEMBERS,setOf(awgID,directID)).commit()
  val root=vless().put("transport","vless").put("dns","10.10.0.1").put("mode",3)
  VpnConfiguration.attachRuntime(c,"default",root)
  val rows=root.getJSONArray(AdaptiveGroups.CANDIDATES)
  assertEquals(setOf("vless","awg","https"),(0 until rows.length()).map{rows.getJSONObject(it).getString("transport")}.toSet())
  for(i in 0 until rows.length()){
   val row=rows.getJSONObject(i);val gateway=AdaptiveGroups.gateway(row,root)
   assertEquals("10.10.0.1",gateway.getString("dns"));assertEquals(3,gateway.getInt("mode"));assertFalse(gateway.has(AdaptiveGroups.CANDIDATES))
   when(row.getString("transport")){
    "awg"->{assertEquals(awg(),gateway.getString("awg_config"));assertFalse(gateway.has("vless_config"));assertFalse(gateway.has("certificate"))}
    "https"->{assertEquals("test-cert",gateway.getString("certificate"));assertFalse(gateway.has("vless_config"));assertFalse(gateway.has("awg_config"))}
    "vless"->{assertTrue(gateway.has("vless_config"));assertFalse(gateway.has("awg_config"))}
   }
  }
 }
 @Test fun awgRootCanUseVlessMember()=fixture{c->
  c.getSharedPreferences("vpn",0).edit().putStringSet(AdaptiveGroups.MEMBERS,emptySet()).commit()
  c.getSharedPreferences("vpn_$awgID",0).edit().putStringSet(AdaptiveGroups.MEMBERS,setOf("default")).commit()
  val root=JSONObject().put("transport","awg").put("awg_config",awg()).put("dns","10.10.0.1")
  VpnConfiguration.attachRuntime(c,awgID,root)
  assertEquals("awg",root.getString("transport"));assertEquals(2,root.getJSONArray(AdaptiveGroups.CANDIDATES).length())
 }
 @Test fun haUsesAwgAndVlessToTheSameMultiplexor()=fixture{c->
  c.getSharedPreferences("vpn",0).edit().putString("relay_exit_profile_id",targetID).commit()
  val root=vless().put("transport","vless");VpnConfiguration.attachRuntime(c,"default",root)
  val gateways=root.getJSONObject("_exit_runtime").getJSONObject("gateways")
  val paths=gateways.keys().asSequence().map{gateways.getJSONObject(it)}.toList()
  assertEquals(2,paths.size);assertEquals(1,paths.count{it.has("underlay_awg_config")});assertEquals(1,paths.count{it.has("underlay_vless_config")})
  for(path in paths){assertEquals("mux.example:443",path.getString("endpoint"));assertEquals("test-cert",path.getString("certificate"));assertFalse(path.has("awg_config"));assertFalse(path.has("vless_config"))}
 }
 @Test fun haRejectsTargetForbiddenByMdm()=fixture{c->
  c.getSharedPreferences("vpn",0).edit().putStringSet(AdaptiveGroups.MEMBERS,emptySet()).putString("relay_exit_profile_id",targetID).commit()
  MdmStore(c).edit{it.put("binding",MdmStore.bindingJson(MdmBinding("mixed-test","https://mdm.example",1,MdmRights(config=true),true))).put("active",true).put("rights",MdmRights(config=true).json())}
  assertFalse(MdmConfiguration.canSelectProfile(c,targetID))
  assertTrue("Denied target must not become an HA exit",runCatching{VpnConfiguration.attachRuntime(c,"default",vless().put("transport","vless"))}.isFailure)
 }
 @Test fun haAllowsOwnQuicAndHttpsWithDistinctPorts()=fixture{c->
  c.getSharedPreferences("vpn",0).edit().putStringSet(AdaptiveGroups.MEMBERS,setOf(directID)).putString("relay_exit_profile_id",targetID).commit()
  c.getSharedPreferences("vpn_$targetID",0).edit().putString("quic_endpoint","mux.example:444").commit()
  c.getSharedPreferences("vpn_$directID",0).edit().putString("transport","quic").putString("endpoint","mux.example:444").commit()
  val root=vless().put("transport","vless");VpnConfiguration.attachRuntime(c,"default",root)
  val runtime=root.getJSONObject("_exit_runtime")
  assertEquals("mux.example:444",runtime.getJSONObject("target_endpoints").getString("quic"))
  val gateways=runtime.getJSONObject("gateways")
  assertEquals(setOf("https","quic"),gateways.keys().asSequence().map{gateways.getJSONObject(it).getString("transport")}.toSet())
  val resolved=mutableListOf<String>();VlessRelayRuntime.resolve(runtime){resolved.add(it);"192.0.2.1:443"}
  assertEquals(setOf("proxy.example:443","mux.example:444"),resolved.toSet())
 }
 @Test fun haRejectsDirectCandidateFromAnotherExit()=fixture{c->
  c.getSharedPreferences("vpn",0).edit().putStringSet(AdaptiveGroups.MEMBERS,setOf(directID)).putString("relay_exit_profile_id",targetID).commit()
  c.getSharedPreferences("vpn_$directID",0).edit().putString("endpoint","other.example:443").putString("https_endpoint","other.example:443").commit()
  assertTrue(runCatching{VpnConfiguration.attachRuntime(c,"default",vless().put("transport","vless"))}.isFailure)
 }
}
