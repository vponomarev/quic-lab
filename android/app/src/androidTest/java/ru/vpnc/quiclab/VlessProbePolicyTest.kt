package ru.vpnc.quiclab
import org.json.JSONObject
import org.junit.Test
import org.junit.Assert.*
class VlessProbePolicyTest {
 @Test fun localPlanKeepsWifiBeforeCellAndNeverAddsWifiAfterCell(){
  assertEquals(listOf("wifi","cell"),VlessProbePolicy.phasePlan("wifi"))
  assertEquals(listOf("cell"),VlessProbePolicy.phasePlan("cell"))
  assertEquals(emptyList<String>(),VlessProbePolicy.phasePlan("unknown"))
 }
 @Test fun primaryCellDoesNotRequireReservePermissionButAlwaysHonorsBudget(){
  assertEquals("",VlessProbePolicy.phaseBlockReason("cell","cell",false,false))
  assertEquals("permission_denied",VlessProbePolicy.phaseBlockReason("cell","wifi",false,false))
  assertEquals("budget",VlessProbePolicy.phaseBlockReason("cell","cell",true,true))
  assertEquals("budget",VlessProbePolicy.phaseBlockReason("cell","wifi",true,true))
  assertEquals("",VlessProbePolicy.phaseBlockReason("wifi","wifi",false,true))
 }
 @Test fun pinnedPhaseOnlyStopsWhenItsNetworkDisappears(){
  assertFalse(VlessProbePolicy.pinnedNetworkChanged(true))
  assertTrue(VlessProbePolicy.pinnedNetworkChanged(false))
 }
 @Test fun schemaTwoSnapshotsStayLocalUntilServerSupportsThem(){
  assertTrue(VlessProbePolicy.reportableV1(JSONObject()))
  assertTrue(VlessProbePolicy.reportableV1(JSONObject().put("schema",1)))
  assertFalse(VlessProbePolicy.reportableV1(JSONObject().put("schema",2)))
 }
 @Test fun canceledAcquisitionDoesNotRequestOrOwnAnyNetwork(){
  val context=androidx.test.platform.app.InstrumentationRegistry.getInstrumentation().targetContext
  try {
   VlessProbeNetwork.acquire(context,"cell"){true}
   fail("Canceled acquisition returned a lease")
  } catch(expected:InterruptedException) {
   assertEquals("canceled",expected.message)
  }
 }
 private fun state(id:String="b",epoch:Long=1,generation:Long=1)=MdmState(MdmBinding(id,"https://example.com",epoch,MdmRights(),true),true,generation,MdmRights(vpn=true,telemetry=true))
 @Test fun remoteOwnershipSurvivesOnlySameEnrollmentAndEpoch(){
  val r=JSONObject().put("initiator","mdm").put("ownerBinding","b").put("ownerEpoch",1).put("ownerGeneration",1)
  assertTrue(VlessProbePolicy.owns(r,state()))
  assertFalse(VlessProbePolicy.owns(r,state(epoch=2)))
  assertFalse(VlessProbePolicy.owns(r,state(id="new")))
  assertFalse(VlessProbePolicy.owns(r,state(generation=2)))
  assertFalse(VlessProbePolicy.owns(r,state().copy(active=false)))
 }
 @Test fun exhaustedBudgetDoesNotBlameHealthyNode(){
  for(outcome in listOf("transport","timeout","relay_failed"))assertEquals("budget",VlessProbePolicy.normalize(JSONObject().put("outcome",outcome),true,true).getString("outcome"))
  assertEquals("success",VlessProbePolicy.normalize(JSONObject().put("outcome","success"),true,true).getString("outcome"))
  assertEquals("transport",VlessProbePolicy.normalize(JSONObject().put("outcome","transport"),false,true).getString("outcome"))
 }
 @Test fun changingActiveNetworkStopsEvenWhenOldNetworkRemains(){
  assertTrue(VlessProbePolicy.networkChanged(1,2,true));assertTrue(VlessProbePolicy.networkChanged(1,null,false));assertFalse(VlessProbePolicy.networkChanged(1,1,true))
 }
}
