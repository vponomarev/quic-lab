package ru.vpnc.quiclab

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

class VlessProbeSequenceTest {
 private fun run()=JSONObject().put("runId","test").put("sourceId","source").put("rows",JSONArray().put(JSONObject().put("id","node").put("outcome","queued")))
 @Test fun finalSaveKeepsBusyGateUntilCleanupReturns(){
  val entered=CountDownLatch(1);val release=CountDownLatch(1)
  val sequence=VlessProbeSequence{if(it.optString("state")=="finished"){entered.countDown();release.await(3,TimeUnit.SECONDS)}}
  sequence.start(run(),listOf("cell")){ProbePhase({JSONObject().put("outcome","success")},{})}
  assertTrue(entered.await(2,TimeUnit.SECONDS))
  try{assertTrue(sequence.busy())}finally{release.countDown()}
 }
 @Test fun retryIncludesFailuresFromEitherNetwork(){
  fun phase(outcome:String)=JSONObject().put("rows",JSONArray().put(JSONObject().put("id","node").put("outcome",outcome)))
  val old=JSONObject().put("rows",phase("success").getJSONArray("rows")).put("phases",JSONObject().put("wifi",phase("timeout")).put("cell",phase("success")))
  assertEquals(setOf("node"),probeRetryIds(old))
 }
 @Test fun budgetCancellationKeepsNeutralReason(){
  val entered=CountDownLatch(1);val engine=VlessProbeEngine{}
  engine.start(run().put("state","running")){entered.countDown();Thread.sleep(3000);JSONObject()}
  assertTrue(entered.await(2,TimeUnit.SECONDS));engine.cancel("test","budget")
  assertEquals("budget",engine.snapshot()!!.getJSONArray("rows").getJSONObject(0).getString("outcome"))
 }
 @Test fun skippedCellKeepsSuccessfulWifi() {
  val done=CountDownLatch(1);val sequence=VlessProbeSequence{if(it.optString("state")=="finished")done.countDown()}
  sequence.start(run(),listOf("wifi","cell")){kind->if(kind=="cell")throw ProbePhaseSkipped("permission_denied");ProbePhase({JSONObject().put("outcome","success")},{})}
  assertTrue(done.await(3,TimeUnit.SECONDS));val phases=sequence.snapshot()!!.getJSONObject("phases")
  assertEquals("success",phases.getJSONObject("wifi").getJSONArray("rows").getJSONObject(0).getString("outcome"))
  assertEquals("skipped",phases.getJSONObject("cell").getString("state"))
  assertEquals("permission_denied",phases.getJSONObject("cell").getString("reason"))
 }
 @Test fun cellOnlyDoesNotAcquireWifi() {
  val done=CountDownLatch(1);val opened=mutableListOf<String>();val sequence=VlessProbeSequence{if(it.optString("state")=="finished")done.countDown()}
  sequence.start(run(),listOf("cell")){kind->opened.add(kind);ProbePhase({JSONObject().put("outcome","success")},{})}
  assertTrue(done.await(3,TimeUnit.SECONDS));assertEquals(listOf("cell"),opened)
 }
 @Test fun closesWifiBeforeAcquiringCell() {
  val events=java.util.Collections.synchronizedList(mutableListOf<String>())
  val done=CountDownLatch(1)
  val sequence=VlessProbeSequence{if(it.optString("state")=="finished")done.countDown()}
  assertTrue(sequence.start(run(),listOf("wifi","cell")){kind->
   events.add("open:$kind")
   ProbePhase({JSONObject().put("outcome","success")},{events.add("close:$kind")})
  })
  assertTrue(done.await(5,TimeUnit.SECONDS))
  assertEquals(listOf("open:wifi","close:wifi","open:cell","close:cell"),events.toList())
  assertEquals("success",sequence.snapshot()!!.getJSONObject("phases").getJSONObject("wifi").getJSONArray("rows").getJSONObject(0).getString("outcome"))
 }
 @Test fun cancelDuringAcquisitionPreventsWorkAndNextPhase() {
  val entered=CountDownLatch(1);val release=CountDownLatch(1);val closed=CountDownLatch(1)
  val sequence=VlessProbeSequence{}
  var opens=0;var works=0
  sequence.start(run(),listOf("wifi","cell")){
   opens++;entered.countDown();release.await()
   ProbePhase({works++;JSONObject().put("outcome","success")},{closed.countDown()})
  }
  assertTrue(entered.await(2,TimeUnit.SECONDS));assertTrue(sequence.cancel("test"));release.countDown()
  assertTrue(closed.await(2,TimeUnit.SECONDS));assertEquals(1,opens);assertEquals(0,works)
  assertEquals("canceled",sequence.snapshot()!!.getString("state"))
 }
}
