package ru.vpnc.quiclab

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class VlessProbePresentationTest {
 private fun row(outcome:String, relay:String="not_applicable")=JSONObject().put("outcome",outcome).put("relayOutcome",relay)
 @Test fun vlessSuccessRemainsVisibleWhenHaFails(){
  val result=VlessProbePresentation.classify(row("relay_failed","relay_auth"))
  assertEquals(ProbeHealth.LIVE,result.vless)
  assertEquals(ProbeHealth.FAILED,result.ha)
 }
 @Test fun untestedAndUnavailableAreNeutral(){
  for(code in listOf("queued","running","canceled","interrupted","network_changed","configuration_changed","permission_denied","no_network","budget","skipped")){
   assertEquals(code,ProbeHealth.UNKNOWN,VlessProbePresentation.classify(row(code,"queued")).vless)
  }
  assertEquals(ProbeHealth.NOT_APPLICABLE,VlessProbePresentation.classify(row("success")).ha)
  assertEquals(ProbeHealth.UNKNOWN,VlessProbePresentation.classify(row("success","not_attempted")).ha)
  assertEquals(ProbeHealth.UNKNOWN,VlessProbePresentation.classify(row("timeout").put("haConfigured",true)).ha)
 }
 @Test fun failuresAndSuccessAreIndependent(){
  assertEquals(ProbeHealth.FAILED,VlessProbePresentation.classify(row("timeout")).vless)
  assertEquals(ProbeHealth.LIVE,VlessProbePresentation.classify(row("success","success")).ha)
  assertEquals(ProbeHealth.LIVE,VlessProbePresentation.classify(row("success")).vless)
 }
 @Test fun networkSelectionNeverMixesMeasurements(){
  val wifi=JSONObject().put("rows",JSONArray().put(row("success")))
  val cell=JSONObject().put("state","skipped").put("reason","budget").put("rows",JSONArray().put(row("budget")))
  val run=JSONObject().put("network","cell").put("phases",JSONObject().put("wifi",wifi).put("cell",cell))
  assertEquals(listOf("wifi","cell"),VlessProbePresentation.networks(run))
  assertEquals("success",VlessProbePresentation.rows(run,"wifi").single().getString("outcome"))
  assertEquals("budget",VlessProbePresentation.rows(run,"cell").single().getString("outcome"))
  val legacy=JSONObject().put("network","wifi").put("rows",JSONArray().put(row("success")))
  assertEquals(1,VlessProbePresentation.rows(legacy,"wifi").size)
  assertTrue(VlessProbePresentation.rows(legacy,"cell").isEmpty())
 }
 @Test fun countsAndFiltersAgreeIncludingPartialSuccess(){
  val rows=listOf(row("success"),row("relay_failed","relay_auth"),row("dns"),row("budget"))
  assertEquals(listOf(2,1,1),VlessProbePresentation.counts(rows))
  assertEquals(2,VlessProbePresentation.filter(rows,1).size)
  assertEquals("dns",VlessProbePresentation.filter(rows,2).single().getString("outcome"))
  assertEquals("budget",VlessProbePresentation.filter(rows,3).single().getString("outcome"))
 }
}
