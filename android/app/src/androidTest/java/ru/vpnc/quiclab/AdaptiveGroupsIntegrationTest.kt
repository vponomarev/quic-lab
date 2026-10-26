package ru.vpnc.quiclab

import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.UUID
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class AdaptiveGroupsIntegrationTest {
 private class Isolated(base:Context):ContextWrapper(base){
  private val prefix="groups-test-${UUID.randomUUID()}"
  private val names=mutableSetOf<String>()
  private val directory=File(base.cacheDir,prefix).apply{check(mkdirs())}
  override fun getFilesDir()=directory
  override fun getNoBackupFilesDir()=directory
  override fun getSharedPreferences(name:String,mode:Int):SharedPreferences {names.add(prefix+name);return baseContext.getSharedPreferences(prefix+name,mode)}
  fun clean(){names.forEach{baseContext.deleteSharedPreferences(it)};check(directory.parentFile==baseContext.cacheDir);directory.deleteRecursively()}
 }
 private val member="11111111-1111-4111-8111-111111111111"
 private val exit="22222222-2222-4222-8222-222222222222"
 private fun uri(host:String)="vless://11111111-1111-4111-8111-111111111111@$host:443?security=tls&type=tcp&sni=example.com"
 private fun source(vararg hosts:String)=JSONObject().put("vless_uris",JSONArray(hosts.map{uri(it)})).put("vless_config",mobile.Mobile.importVLESSConfig(uri(hosts.first())))
 private fun withGroup(block:(Isolated)->Unit){
  val c=Isolated(InstrumentationRegistry.getInstrumentation().targetContext)
  try{
   check(c.noBackupFilesDir==c.filesDir && c.noBackupFilesDir!=c.baseContext.noBackupFilesDir)
   check(!MdmConfiguration.hasLayer(c)){"Test configuration directory is not isolated"}
   c.getSharedPreferences("vpn_profiles",0).edit().putString("profiles",JSONArray().put(JSONObject().put("id","default").put("name","Group")).put(JSONObject().put("id",member).put("name","B")).put(JSONObject().put("id",exit).put("name","Exit")).toString()).putString("current","default").commit()
   c.getSharedPreferences("vpn",0).edit().putString("transport","vless").putString("endpoint","a1.example:443").putStringSet(AdaptiveGroups.MEMBERS,setOf(member)).commit()
   c.getSharedPreferences("vpn_$member",0).edit().putString("transport","vless").putString("endpoint","b1.example:443").commit()
   c.getSharedPreferences("vpn_$exit",0).edit().putString("transport","https").putString("endpoint","gateway.example:443").putString("https_endpoint","gateway.example:443").commit()
   VpnIdentity.writeBundle(c,member,source("b1.example"))
   VpnIdentity.writeBundle(c,exit,JSONObject().put("certificate","cert").put("key","key"))
   block(c)
  }finally{c.clean()}
 }
 @Test fun normalGroupUsesBothSourcesWithoutChangingModeOrRouting()=withGroup{c->
  val config=source("a1.example","a2.example").put("transport","vless").put("dns","10.0.0.1")
  VpnConfiguration.attachRuntime(c,"default",config)
  assertEquals("vless",config.getString("transport"));assertEquals("10.0.0.1",config.getString("dns"));assertFalse(config.has("_exit_runtime"))
  val candidates=config.getJSONArray(AdaptiveGroups.CANDIDATES)
  assertEquals(listOf("default",member,"default"),(0 until candidates.length()).map{candidates.getJSONObject(it).getString("connection_id")})
  assertEquals("b1.example:443",JSONObject(candidates.getJSONObject(1).getString("config")).getJSONObject("config").getString("Endpoint"))
 }
 @Test fun haGroupProjectsAllMembersToOneExit()=withGroup{c->
  c.getSharedPreferences("vpn",0).edit().putString("relay_exit_profile_id",exit).commit()
  val config=source("a1.example","a2.example").put("transport","vless")
  VpnConfiguration.attachRuntime(c,"default",config)
  val runtime=config.getJSONObject("_exit_runtime")
  assertEquals(1,runtime.getJSONObject("model").getJSONArray("exits").length())
  val gateways=runtime.getJSONObject("gateways")
  assertEquals(3,gateways.length())
  val connections=runtime.getJSONObject("logical_connections")
  assertEquals(setOf("default",member),connections.keys().asSequence().map{connections.getString(it)}.toSet())
  for(id in gateways.keys()){assertEquals("gateway.example:443",gateways.getJSONObject(id).getString("endpoint"));assertTrue(gateways.getJSONObject(id).has("underlay_vless_config"))}
 }
 @Test fun rejectsNestedAndUnsupportedMembers()=withGroup{c->
  val p=c.getSharedPreferences("vpn_$member",0)
  p.edit().putString("transport","awg").commit()
  assertTrue(runCatching{VpnConfiguration.attachRuntime(c,"default",source("a.example"))}.isFailure)
  p.edit().putString("transport","vless").putStringSet(AdaptiveGroups.MEMBERS,setOf("default")).commit()
  assertTrue(runCatching{VpnConfiguration.attachRuntime(c,"default",source("a.example"))}.isFailure)
 }
 @Test fun rejectsMemberAlsoEnabledAsMultipleExit()=withGroup{c->
  c.getSharedPreferences("vpn_profiles",0).edit().putBoolean("multiple",true).putStringSet("multiple_enabled",setOf("default",member)).commit()
  assertTrue(runCatching{VpnConfiguration.attachRuntime(c,"default",source("a.example"))}.isFailure)
 }
 @Test fun membershipPersistsInPersonalRuntimeLayer()=withGroup{c->
  val doc=MdmConfiguration.snapshotLocal(c)
  val runtime=MdmConfiguration.snapshotRuntime(c)
  MdmConfigurationStore(c).initialize(doc,runtime)
  assertTrue(MdmConfiguration.hasLayer(c))
  VpnProfiles.preferences(c,"default").edit().putStringSet(AdaptiveGroups.MEMBERS,setOf(member)).commit()
  assertEquals(setOf(member),VpnProfiles.preferences(c,"default").getStringSet(AdaptiveGroups.MEMBERS,emptySet()))
  val config=source("a.example").put("transport","vless")
  VpnConfiguration.attachRuntime(c,"default",config)
  assertEquals(2,config.getJSONArray(AdaptiveGroups.CANDIDATES).length())
  assertFalse(MdmConfiguration.snapshotLocal(c).getJSONArray("profiles").getJSONObject(0).getJSONObject("settings").has(AdaptiveGroups.MEMBERS))
 }
 @Test fun individualSubscriptionIdentityReachesRuntimeCandidates()=withGroup{c->
  val candidates=importedSubscriptionCandidates(c,2)
  val logical=(0 until candidates.length()).map{candidates.getJSONObject(it).getString("connection_id")}
  assertEquals(2,logical.count{it=="subscription.33333333-3333-4333-8333-333333333333"})
  assertEquals(1,logical.count{it==member})
 }
 @Test fun profileCountDoesNotTurnOneSubscriptionIntoManyConnections()=withGroup{c->
  val candidates=importedSubscriptionCandidates(c,17)
  assertEquals(18,candidates.length())
 }
 @Test fun overlappingGroupedAndIndividualImportsDoNotDuplicateRetries()=withGroup{c->
  assertEquals(3,importedSubscriptionCandidates(c,2,true).length())
 }
 private fun importedSubscriptionCandidates(c:Isolated,count:Int,includeGrouped:Boolean=false):JSONArray {
  val group="33333333-3333-4333-8333-333333333333"
  val parsed=VlessSubscriptions.parse((0 until count).joinToString("\n"){uri("sub-$it.example")}.toByteArray())
  val imported=VlessSubscriptions.merge(JSONObject().put("profiles",JSONArray()),group,parsed).getJSONArray("profiles")
  val first=imported.getJSONObject(0).getString("id")
  val others=((1 until imported.length()).map{imported.getJSONObject(it).getString("id")}.toSet()+member).toMutableSet()
  val list=JSONArray(c.getSharedPreferences("vpn_profiles",0).getString("profiles","[]"))
  for(i in 0 until imported.length()){
   val p=imported.getJSONObject(i);val id=p.getString("id")
   list.put(JSONObject().put("id",id).put("name","Subscription node"))
   c.getSharedPreferences("vpn_$id",0).edit().putString("transport","vless").putString("endpoint",p.getJSONObject("settings").getString("endpoint")).commit()
   VpnIdentity.writeBundle(c,id,JSONObject().put("vless_config",mobile.Mobile.importVLESSConfig(p.getJSONObject("identity").getString("vless_uri"))))
  }
  if(includeGrouped){
   val groupedId=java.util.UUID.nameUUIDFromBytes(("vless-group:"+group).toByteArray()).toString()
   list.put(JSONObject().put("id",groupedId).put("name","Same subscription group"))
   c.getSharedPreferences("vpn_$groupedId",0).edit().putString("transport","vless").putString("endpoint","sub-0.example:443").commit()
   VpnIdentity.writeBundle(c,groupedId,source(*(0 until count).map{"sub-$it.example"}.toTypedArray()))
   others.add(groupedId)
  }
  c.getSharedPreferences("vpn_profiles",0).edit().putString("profiles",list.toString()).commit()
  c.getSharedPreferences("vpn_$first",0).edit().putStringSet(AdaptiveGroups.MEMBERS,others).commit()
  val runtime=MdmConfiguration.snapshotRuntime(c).put("subscriptions",JSONObject().put(group,JSONObject().put("selected",JSONArray(parsed.entries.map{it.key}))))
  MdmConfigurationStore(c).initialize(MdmConfiguration.snapshotLocal(c),runtime)
  val config=source("sub-0.example").put("transport","vless")
  VpnConfiguration.attachRuntime(c,first,config)
  return config.getJSONArray(AdaptiveGroups.CANDIDATES)
 }
}
