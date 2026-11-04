package ru.vpnc.quiclab

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean

class MdmProbeSequenceTest {
 @Test fun everyPollAdvertisesSequenceParserCapability(){
  for(wait in listOf(true,false)) {
   val request=MdmSyncRequest("binding",1,wait=wait).json()
   assertEquals(listOf("vless_probe_sequence_v2"),request.getJSONArray("capabilities").let{a->(0 until a.length()).map{a.getString(it)}})
  }
 }
 private fun source(id:String,count:Int=1)=ProbeSource(id,id,(0 until count).map{ProbeNode("node$it","Node","example.org:443",JSONObject().put("identity",id))})
 private val owner=MdmState(MdmBinding("binding","https://mdm.example",1,MdmRights(),true),true,7,MdmRights(vpn=true,telemetry=true))
 private fun run()=JSONObject().put("runId","run").put("sourceId","s").put("initiator","mdm").put("ownerBinding","binding").put("ownerEpoch",1).put("ownerGeneration",7).put("rows",JSONArray().put(JSONObject().put("id","node").put("outcome","queued")))
 private fun awaitStop(sequence:VlessProbeSequence){val end=System.nanoTime()+TimeUnit.SECONDS.toNanos(3);while(sequence.busy()&&System.nanoTime()<end)Thread.sleep(5);assertFalse(sequence.busy())}
 @Test fun aggregateUsesFullCatalogueWithin32SlotsAndOldServerKeepsLegacy(){
  val sources=(0 until 33).map{source("s$it")};val catalogue=MdmProbeReport.catalogue(sources,true)
  assertEquals(32,catalogue.size);val all=catalogue.single{it.id==ALL_PROBE_SOURCE_ID};assertEquals(33,all.nodes.size)
  assertEquals(allProbeSource(sources)!!.fingerprint,all.fingerprint);assertEquals(32,MdmProbeReport.catalogue(sources,false).size)
  assertFalse(MdmProbeReport.catalogue(sources,false).any{it.id==ALL_PROBE_SOURCE_ID})
  assertFalse(MdmProbeReport.catalogue(listOf(source("large",257)),true).any{it.id==ALL_PROBE_SOURCE_ID})
  assertTrue(MdmProbeReport.catalogue(emptyList(),true).isEmpty())
  assertNotEquals(all.fingerprint,MdmProbeReport.catalogue(sources.dropLast(1),true).first().fingerprint)
 }
 @Test fun remoteSnapshotNeedsNegotiatedSequenceAndCurrentOwner(){
  val src=source("s");val r=run().put("fingerprint",src.fingerprint).put("schema",2).put("sequence",1).put("state","running").put("startedAt","2026-11-04T00:00:00Z")
  r.put("phases",JSONObject().put("wifi",JSONObject().put("network","wifi").put("state","queued").put("rows",JSONArray().put(src.nodes.single().publicRow()))))
  val all=JSONObject().put("s",r)
  assertNull(MdmProbeReport.select(all,listOf(src),owner,true,false))
  assertNotNull(MdmProbeReport.select(all,listOf(src),owner,true,true))
  assertNull(MdmProbeReport.select(all,listOf(src),owner.copy(localGeneration=8),true,true))
 }
 @Test fun newCommandRequiresCapabilityAndHasSeparateKind(){
  val raw=JSONObject("""{"version":1,"epoch":1,"serverTime":"2026-11-04T00:00:00Z","capabilities":["vless_probe_v1","vless_probe_phases_v2","vless_probe_sequence_v2"],"commands":[{"id":"cmd","bindingId":"binding","epoch":1,"issuedAt":"2026-11-04T00:00:00Z","expiresAt":"2026-11-04T00:05:00Z","kind":"vless_probe_sequence","probe":{"mode":"phased","action":"start","sourceId":"s","fingerprint":"fp"}}]}""")
  val parsed=MdmSyncResponse.parse(raw.toString());assertTrue(parsed.probeSequenceSupported);assertEquals("vless_probe_sequence",parsed.commands.single().kind)
  raw.put("capabilities",JSONArray().put("vless_probe_v1"));assertTrue(runCatching{MdmSyncResponse.parse(raw.toString())}.isFailure)
 }
 @Test fun revocationDuringAcquisitionClosesLeaseWithoutWorkOrNextPhase(){
  val allowed=AtomicBoolean(true);val entered=CountDownLatch(1);val release=CountDownLatch(1);var opens=0;var works=0;var closes=0
  val sequence=VlessProbeSequence{}
  sequence.start(run(),listOf("wifi","cell"),allowed={allowed.get()}){opens++;entered.countDown();release.await(3,TimeUnit.SECONDS);ProbePhase({works++;JSONObject().put("outcome","success")},{closes++})}
  assertTrue(entered.await(2,TimeUnit.SECONDS));allowed.set(false);release.countDown();awaitStop(sequence)
  assertEquals(1,opens);assertEquals(0,works);assertEquals(1,closes);assertEquals("permission_denied",sequence.snapshot()!!.getString("state"))
 }
 @Test fun revocationBetweenPhasesDoesNotAcquireCell(){
  val allowed=AtomicBoolean(true);val opened=mutableListOf<String>();val sequence=VlessProbeSequence{}
  sequence.start(run(),listOf("wifi","cell"),allowed={allowed.get()}){kind->opened.add(kind);ProbePhase({JSONObject().put("outcome","success")},{allowed.set(false)})}
  awaitStop(sequence);assertEquals(listOf("wifi"),opened);assertEquals("permission_denied",sequence.snapshot()!!.getString("state"))
 }
 @Test fun revokedBeforeStartAcquiresNothing(){
  var opens=0;val sequence=VlessProbeSequence{}
  sequence.start(run(),listOf("wifi","cell"),allowed={false}){opens++;ProbePhase({JSONObject()},{})}
  awaitStop(sequence);assertEquals(0,opens);assertEquals("permission_denied",sequence.snapshot()!!.getString("state"))
 }
 @Test fun phasedCommandsCannotCrossSchemaSourceOrOwnerGeneration(){
  val now=java.time.Instant.parse("2026-11-04T00:00:00Z")
  fun command(kind:String="vless_probe_sequence",mode:String="phased",action:String="cancel")=MdmCommand("cmd","binding",1,now,now.plusSeconds(300),kind,JSONObject().put("mode",mode).put("action",action).put("runId","run").put("sourceId","s").put("fingerprint","fp"))
  val saved=JSONObject().put("s",run().put("schema",2).put("fingerprint","fp"))
  assertEquals("",MdmProbeProtocol.targetError(command(),saved,owner))
  assertEquals("unsupported",MdmProbeProtocol.targetError(command("vless_probe",""),saved,owner))
  assertEquals("configuration_changed",MdmProbeProtocol.targetError(command(),saved,owner.copy(localGeneration=8)))
  saved.getJSONObject("s").put("sourceId","other");assertEquals("configuration_changed",MdmProbeProtocol.targetError(command(),saved,owner))
  assertEquals("invalid",MdmProbeProtocol.targetError(command(mode=""),saved,owner))
  assertEquals("",MdmProbeProtocol.targetError(command(action="start"),JSONObject(),owner))
 }
}
