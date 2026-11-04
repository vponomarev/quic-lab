package ru.vpnc.quiclab

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class MdmProbeReportTest {
 private val source=ProbeSource("group","Group",listOf(ProbeNode("node","Node","example.org:443",JSONObject().put("secret","never-send"))))
 private val state=MdmState(MdmBinding("binding","https://mdm.example",1,MdmRights(),true),true,7,MdmRights(vpn=true,telemetry=true))
 private fun run(schema:Int=2):JSONObject {
  val row=source.nodes.single().publicRow().put("outcome","success").put("password","never-send")
  val phases=JSONObject().put("wifi",JSONObject().put("network","wifi").put("state","finished").put("rows",JSONArray().put(row)).put("ownerBinding","never-send"))
   .put("cell",JSONObject().put("network","cell").put("state","skipped").put("reason","permission_denied").put("rows",JSONArray().put(JSONObject(row.toString()).put("outcome","permission_denied"))))
  return JSONObject().put("schema",schema).put("runId","run").put("sourceId",source.id).put("fingerprint",source.fingerprint).put("sequence",2).put("network","cell").put("initiator","local").put("state","finished").put("startedAt","2026-11-04T00:00:00Z").put("rows",JSONArray().put(row)).put("phases",phases).put("ownerBinding","never-send").put("config","never-send")
 }
 private fun select(r:JSONObject,v2:Boolean=true,s:MdmState=state)=MdmProbeReport.select(JSONObject().put(source.id,r),listOf(source),s,v2)
 @Test fun oldServerOmitsPhasesAndLegacyProjectionHasOnlyPublicFields(){
  assertNull(select(run(),false))
  val legacy=select(run(1).apply{remove("phases")},false)!!
  assertFalse(legacy.has("schema"));assertFalse(legacy.has("phases"));assertFalse(legacy.toString().contains("never-send"))
  assertEquals("success",legacy.getJSONArray("rows").getJSONObject(0).getString("outcome"))
 }
 @Test fun negotiatedPhasesKeepBothNetworksAndNeutralReason(){
  val raw=run();val before=raw.toString();val result=select(raw)!!
  assertEquals(2,result.getInt("schema"));assertFalse(result.has("rows"));assertEquals(before,raw.toString())
  val phases=result.getJSONObject("phases");assertEquals("success",phases.getJSONObject("wifi").getJSONArray("rows").getJSONObject(0).getString("outcome"))
  assertEquals("permission_denied",phases.getJSONObject("cell").getString("reason"));assertFalse(result.toString().contains("never-send"))
 }
 @Test fun schemaSourceAndConsentFilter(){
  assertNull(select(run(3)));assertNull(select(run().put("fingerprint","changed")))
  assertNull(select(run(),s=state.copy(active=false)));assertNull(select(run(),s=state.copy(cleanupPending=true)))
  assertNull(select(run(),s=state.copy(rights=MdmRights(vpn=true))))
 }
 @Test fun oldRemoteOwnershipMustMatchAndRemoteV2IsNotAdvertised(){
  val r=run(1).apply{remove("phases")}.put("initiator","mdm").put("ownerBinding","binding").put("ownerEpoch",1).put("ownerGeneration",7)
  assertNotNull(select(r));r.put("ownerGeneration",6);assertNull(select(r));assertNull(select(run().put("initiator","mdm")))
 }
 @Test fun queuedSequenceHasNoActiveNetworkYet(){val r=run().apply{remove("network")};assertEquals("none",select(r)!!.getString("network"))}
 @Test fun boundedReportKeepsBaseFieldsWhenProbeWouldExceedLimit(){
  val out=JSONObject().put("inventory", "x".repeat(901*1024));val before=out.toString()
  MdmProbeReport.append(out,select(run()));assertEquals(before,out.toString())
  val small=JSONObject().put("sequence",5);MdmProbeReport.append(small,select(run()));assertEquals(1,small.getJSONArray("probeSnapshots").length());assertEquals(5,small.getInt("sequence"))
 }
 @Test fun tooManyRowsOrMalformedPhasesAreNotSent(){
  val r=run();val rows=JSONArray();repeat(257){rows.put(source.nodes.single().publicRow())};r.getJSONObject("phases").getJSONObject("wifi").put("rows",rows);assertNull(select(r))
  assertNull(select(run().put("phases",JSONObject().put("ethernet",JSONObject()))))
 }
 @Test fun capabilityParserDefaultsOffAndRecognizesNegotiation(){
  val raw=JSONObject().put("version",1).put("epoch",1).put("serverTime","2026-11-04T00:00:00Z")
  assertFalse(MdmSyncResponse.parse(raw.toString()).probePhasesSupported)
  raw.put("capabilities",JSONArray().put("vless_probe_phases_v2"));assertTrue(MdmSyncResponse.parse(raw.toString()).probePhasesSupported)
 }
 @Test fun nestedValuesInPublicScalarFieldsAreRejected(){
  for(key in listOf("name","echoRttMs")){
   val r=run();r.getJSONObject("phases").getJSONObject("wifi").getJSONArray("rows").getJSONObject(0).put(key,JSONObject().put("password","private-nested-value"))
   assertNull("nested $key",select(r))
  }
  assertNull(select(run().put("startedAt",JSONArray().put("private-nested-value"))))
 }
 @Test fun legacyRemoteCancelCannotTargetLocalPhasedRun(){
  val base=androidx.test.platform.app.InstrumentationRegistry.getInstrumentation().targetContext
  val dir=java.io.File(base.cacheDir,"probe-report-${java.util.UUID.randomUUID()}").apply{check(mkdirs())}
  val c=object:android.content.ContextWrapper(base){override fun getFilesDir()=dir;override fun getNoBackupFilesDir()=dir;override fun getApplicationContext():android.content.Context=this}
  try{
   val store=MdmStore(c);store.edit{it.put("binding",MdmStore.bindingJson(state.binding!!)).put("active",true).put("generation",7).put("rights",state.rights.json()).put("server_vless_probe_v1",true)}
   VlessProbeStore(c).save(run())
   val now=java.time.Instant.parse("2026-11-04T00:00:00Z")
   val command=MdmCommand("cancel-command","binding",1,now,now.plusSeconds(300),"vless_probe",JSONObject().put("action","cancel").put("runId","run"))
   assertEquals("unsupported",MdmProbeControl.execute(c,store.read(),command,now))
  }finally{check(dir.parentFile==base.cacheDir);dir.deleteRecursively()}
 }
}
