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

class GroupConfigurationTransferTest {
 private class Isolated(base:Context):ContextWrapper(base){
  private val prefix="group-transfer-${UUID.randomUUID()}"
  private val names=mutableSetOf<String>()
  private val dir=File(base.cacheDir,prefix).apply{check(mkdirs())}
  override fun getFilesDir()=dir
  override fun getNoBackupFilesDir()=dir
  override fun getApplicationContext():Context=this
  override fun getSharedPreferences(name:String,mode:Int):SharedPreferences{names.add(prefix+name);return baseContext.getSharedPreferences(prefix+name,mode)}
  fun close(){names.forEach{baseContext.deleteSharedPreferences(it)};check(dir.parentFile==baseContext.cacheDir);dir.deleteRecursively()}
 }
 private val member="11111111-1111-4111-8111-111111111111"
 private val added="22222222-2222-4222-8222-222222222222"
 private val addedMember="33333333-3333-4333-8333-333333333333"
 private fun fixture(block:(Isolated)->Unit){
  val c=Isolated(InstrumentationRegistry.getInstrumentation().targetContext)
  try {
   check(c.filesDir!=c.baseContext.filesDir && c.noBackupFilesDir!=c.baseContext.noBackupFilesDir)
   val profiles=JSONArray().put(JSONObject().put("id","default").put("name","Root")).put(JSONObject().put("id",member).put("name","Member"))
   c.getSharedPreferences("vpn_profiles",0).edit().putString("profiles",profiles.toString()).putString("current","default").commit()
   for(id in listOf("default",member)){
    val host=if(id=="default")"root.example" else "member.example"
    VpnProfiles.preferences(c,id).edit().putString("transport","vless").putString("endpoint","$host:443").commit()
    val uri="vless://11111111-1111-4111-8111-111111111111@$host:443?security=tls&type=tcp&sni=$host"
    VpnIdentity.writeBundle(c,id,JSONObject().put("vless_config",mobile.Mobile.importVLESSConfig(uri)).put("update_token","never-export"))
   }
   VpnProfiles.preferences(c,"default").edit().putStringSet(AdaptiveGroups.MEMBERS,setOf(member)).commit()
   block(c)
  }finally{c.close()}
 }
 private fun managed(c:Context,supported:Boolean){MdmStore(c).edit{
  it.put("binding",MdmStore.bindingJson(MdmBinding("fixture","https://mdm.example",1,MdmRights(config=true),true)))
   .put("active",true).put("rights",MdmRights(config=true).json()).put("generation",7).put("server_automatic_groups_v1",supported)
 }}
 private fun forceGroup(doc:JSONObject)=doc.also{it.getJSONArray("profiles").getJSONObject(0).getJSONObject("settings").put(AdaptiveGroups.MEMBERS,JSONArray().put(member))}
 @Test fun snapshotAndEncryptedRoundTripKeepMembershipAndRemapRuntime()=fixture{c->
  val original=ConfigurationTransfer.snapshot(c,true)
  assertEquals(member,original.getJSONArray("profiles").getJSONObject(0).getJSONObject("settings").getJSONArray(AdaptiveGroups.MEMBERS).getString(0))
  val encrypted=ConfigurationTransfer.export(c,listOf("profiles"),emptyList(),true,"fixture-password")
  val decoded=ConfigurationTransfer.decode(encrypted,"fixture-password")
  assertFalse(decoded.contains("never-export"))
  val preview=ConfigurationTransfer.prepare(c,decoded,"add",mapOf("default" to added,member to addedMember))
  val port=object:MdmVpnPort{override fun active()=false;override fun stop(){fail("unexpected stop")};override fun start():String{fail("unexpected start");return ""}}
  LocalConfigurationApply.apply(c,preview,port)
  assertEquals(setOf(addedMember),VpnProfiles.preferences(c,added).getStringSet(AdaptiveGroups.MEMBERS,emptySet()))
  val runtime=VpnIdentity.load(c,added).put("transport","vless")
  VpnConfiguration.attachRuntime(c,added,runtime)
  assertEquals(2,runtime.getJSONArray(AdaptiveGroups.CANDIDATES).length())
  LocalConfigurationApply.apply(c,ConfigurationTransfer.rollback(c),port)
  assertEquals(listOf("default",member),VpnProfiles.list(c).map{it.id})
  assertEquals(setOf(member),VpnProfiles.preferences(c,"default").getStringSet(AdaptiveGroups.MEMBERS,emptySet()))
 }
 @Test fun partialExportListsExternalMemberAndRelayDependenciesForMapping() {
  val p=JSONObject().put("id","default").put("name","Root").put("settings",JSONObject().put(AdaptiveGroups.MEMBERS,JSONArray().put(member)).put("relay_exit_profile_id",added))
  val names=ConfigurationReferences.names(JSONObject().put("profiles",JSONArray().put(p)))
  assertEquals(setOf("default",member,added),names.keys)
  assertEquals("Root",names["default"])
 }
 @Test fun missingMemberOrRelayPreventsResumingActiveRoot()=fixture{c->
  val d=forceGroup(MdmConfiguration.snapshotLocal(c))
  assertTrue(ConfigurationReferences.incomplete(d,setOf(member)))
  assertFalse(ConfigurationReferences.incomplete(d,setOf(added)))
  d.getJSONArray("profiles").getJSONObject(0).getJSONObject("settings").put("relay_exit_profile_id",added)
  assertTrue(ConfigurationReferences.incomplete(d,setOf(added)))
  d.getJSONObject("dns").put("mode","system")
  d.put("multiple",true).put("enabledProfileIds",JSONArray().put(member))
  assertFalse(ConfigurationReferences.incomplete(d,setOf("default")))
 }
 @Test fun managedGroupUsesDeclaredMemberWithoutGrantingStandaloneSelection()=fixture{c->
  val d=forceGroup(MdmConfiguration.snapshotLocal(c))
  managed(c,true)
  assertTrue(MdmConfiguration.apply(c,MdmConfigRevision(1,"current",d),7))
  assertFalse(MdmConfiguration.canSelectProfile(c,member))
  val config=VpnIdentity.load(c,"default").put("transport","vless")
  VpnConfiguration.attachRuntime(c,"default",config)
  assertEquals(2,config.getJSONArray(AdaptiveGroups.CANDIDATES).length())
 }
 @Test fun legacyServerCurrentRoundTripPreservesGroupAndRejectsDanglingRemoval()=fixture{c->
  val d=MdmConfiguration.snapshotLocal(c)
  d.getJSONArray("profiles").getJSONObject(0).getJSONObject("settings").remove(AdaptiveGroups.MEMBERS)
  managed(c,false)
  assertTrue(MdmConfiguration.apply(c,MdmConfigRevision(1,"current",d),7))
  assertEquals(setOf(member),VpnProfiles.preferences(c,"default").getStringSet(AdaptiveGroups.MEMBERS,emptySet()))
  val before=MdmConfigurationStore(c).effective().toString()
  d.put("profiles",JSONArray().put(d.getJSONArray("profiles").getJSONObject(0)))
  assertTrue(runCatching{MdmConfiguration.apply(c,MdmConfigRevision(2,"current",d),7)}.isFailure)
  assertEquals(before,MdmConfigurationStore(c).effective().toString())
 }
 @Test fun reportAdvertisesCapabilityAndOnlySendsGroupsToSupportingServer()=fixture{c->
  managed(c,false)
  val s=MdmStore(c)
  val old=MdmDeviceReport.snapshot(c,s.read())
  assertTrue(old.getJSONArray("capabilities").toString().contains("automatic_groups_v1"))
  assertFalse(old.getJSONObject("configuration").getJSONArray("profiles").getJSONObject(0).getJSONObject("settings").has(AdaptiveGroups.MEMBERS))
  s.edit{it.put("server_automatic_groups_v1",true)}
  val modern=MdmDeviceReport.snapshot(c,s.read())
  assertEquals(member,modern.getJSONObject("configuration").getJSONArray("profiles").getJSONObject(0).getJSONObject("settings").getJSONArray(AdaptiveGroups.MEMBERS).getString(0))
  assertEquals(setOf(member),VpnProfiles.preferences(c,"default").getStringSet(AdaptiveGroups.MEMBERS,emptySet()))
 }
 @Test fun referencedProfileDeletionFailsBeforeChangingConfiguration()=fixture{c->
  val before=VpnProfiles.list(c)
  assertTrue(runCatching{VpnProfiles.delete(c,member)}.isFailure)
  assertEquals(before,VpnProfiles.list(c));assertTrue(VpnIdentity.exists(c,member))
 }
 @Test fun legacyDanglingUpdateIsRejectedBeforeStoppingVpn()=fixture{c->
  val d=MdmConfiguration.snapshotLocal(c)
  d.getJSONArray("profiles").getJSONObject(0).getJSONObject("settings").remove(AdaptiveGroups.MEMBERS)
  d.put("profiles",JSONArray().put(d.getJSONArray("profiles").getJSONObject(0)))
  managed(c,false)
  var stops=0
  val port=object:MdmVpnPort{override fun active()=true;override fun stop(){stops++};override fun start()="running"}
  val result=MdmApplyCoordinator(c,port).apply(MdmStore(c).read(),MdmConfigRevision(1,"current",d))
  assertFalse(result.configurationApplied)
  assertEquals("invalid merged group disconnected working VPN",0,stops)
  assertFalse(MdmConfiguration.hasLayer(c))
 }
 @Test fun legacyReportDoesNotSendUnrepresentableMixedHaConfiguration()=fixture{c->
  VpnProfiles.preferences(c,"default").edit().putString("transport","awg").putString("relay_exit_profile_id",member).commit()
  VpnProfiles.preferences(c,member).edit().putString("transport","https").putString("https_endpoint","exit.example:443").commit()
  managed(c,false)
  val s=MdmStore(c)
  val report=MdmDeviceReport.snapshot(c,s.read())
  assertFalse("old server cannot represent a mixed HA root",report.has("configuration"))
  assertTrue(report.has("capabilities"))
 }
 @Test fun capabilityHandshakeAndScopeDoNotGrantUnrelatedRoots()=fixture{c->
  val response=MdmSyncResponse.parse("""{"version":1,"epoch":1,"serverTime":"2026-11-04T00:00:00Z","capabilities":["automatic_groups_v1"]}""")
  assertTrue(response.automaticGroupsSupported)
  assertFalse(MdmSyncResponse.parse("""{"version":1,"epoch":1,"serverTime":"2026-11-04T00:00:00Z"}""").automaticGroupsSupported)
  val runtime=MdmConfiguration.snapshotRuntime(c)
  assertTrue(MdmGroupCompatibility.allowsDependency(runtime,"default",member))
  assertFalse(MdmGroupCompatibility.allowsDependency(runtime,member,"default"))
  assertFalse(MdmGroupCompatibility.allowsDependency(runtime,"default",added))
 }
 @Test fun fullReplacementWithMissingMemberDoesNotResumeVpn()=fixture{c->
  val source=ConfigurationTransfer.snapshot(c,true)
  source.getJSONArray("profiles").getJSONObject(1).remove("identity")
  val raw=mobile.Mobile.exportConfiguration(source.toString(),"""{"sections":["profiles","routing","network","diagnostics"]}""",true)
  val preview=ConfigurationTransfer.prepare(c,raw,"replace",mapOf("default" to added,member to addedMember))
  assertEquals(listOf(addedMember),preview.missing)
  var running=true;var starts=0
  val port=object:MdmVpnPort{override fun active()=running;override fun stop(){running=false};override fun start():String{starts++;running=true;return "starting"}}
  LocalConfigurationApply.apply(c,preview,port)
  assertEquals(0,starts);assertFalse(running)
  assertEquals(setOf(addedMember),VpnProfiles.preferences(c,added).getStringSet(AdaptiveGroups.MEMBERS,emptySet()))
 }
 @Test fun existingCredentiallessMemberPreventsResumeAfterPartialUpdate()=fixture{c->
  VpnProfiles.preferences(c,"default").edit().remove(AdaptiveGroups.MEMBERS).commit()
  VpnIdentity.deleteBundle(c,member)
  val source=forceGroup(ConfigurationTransfer.snapshot(c,true))
  val raw=mobile.Mobile.exportConfiguration(source.toString(),"""{"sections":["profiles"],"profileIds":["default"]}""",false)
  val preview=ConfigurationTransfer.prepare(c,raw,"update",mapOf("default" to "default",member to member))
  var starts=0;var running=true
  val port=object:MdmVpnPort{override fun active()=running;override fun stop(){running=false};override fun start():String{starts++;running=true;return "starting"}}
  LocalConfigurationApply.apply(c,preview,port)
  assertEquals("existing member without credentials resumed unusable group",0,starts)
  assertFalse(running)
 }
 @Test fun subscriptionAffinityLossIsRejectedBeforeStopOrCommit()=fixture{c->
  val group="44444444-4444-4444-8444-444444444444"
  val parsed=VlessSubscriptions.parse((0 until 17).joinToString("\n"){"vless://11111111-1111-4111-8111-111111111111@node-$it.example:443?security=tls&type=tcp&sni=example.org"}.toByteArray())
  val source=VlessSubscriptions.merge(ConfigurationTransfer.snapshot(c,true),group,parsed)
  val profiles=source.getJSONArray("profiles")
  val first=profiles.getJSONObject(2).getString("id")
  profiles.getJSONObject(2).getJSONObject("settings").put(AdaptiveGroups.MEMBERS,JSONArray((3 until profiles.length()).map{profiles.getJSONObject(it).getString("id")}))
  val runtime=MdmConfiguration.compile(source,MdmConfiguration.snapshotRuntime(c)).put("subscriptions",JSONObject().put(group,JSONObject().put("selected",JSONArray(parsed.entries.map{it.key}))))
  MdmConfigurationStore(c).initialize(source,runtime)
  val usable=VpnIdentity.load(c,first).put("transport","vless")
  VpnConfiguration.attachRuntime(c,first,usable)
  assertEquals(17,usable.getJSONArray(AdaptiveGroups.CANDIDATES).length())
  val before=ConfigurationTransfer.snapshot(c,true).toString()
  val raw=mobile.Mobile.exportConfiguration(before,"""{"sections":["profiles","routing","network","diagnostics"]}""",true)
  val mapping=(0 until profiles.length()).associate{profiles.getJSONObject(it).getString("id") to UUID.randomUUID().toString()}
  var stops=0
  val port=object:MdmVpnPort{override fun active()=true;override fun stop(){stops++};override fun start()="running"}
  val failed=runCatching{LocalConfigurationApply.apply(c,ConfigurationTransfer.prepare(c,raw,"replace",mapping),port)}.isFailure
  assertTrue("unusable 17-connection group committed after losing private affinity",failed)
  assertEquals(0,stops)
  assertEquals(before,ConfigurationTransfer.snapshot(c,true).toString())
 }
}
