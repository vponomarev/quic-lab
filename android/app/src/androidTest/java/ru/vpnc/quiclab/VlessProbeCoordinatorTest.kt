package ru.vpnc.quiclab
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Test
import org.junit.Assert.*
import org.junit.runner.RunWith
import org.json.JSONObject
import org.json.JSONArray
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger
@RunWith(AndroidJUnit4::class)
class VlessProbeCoordinatorTest{
 @Test fun staleLegacyCancelCannotClearNewLocalOwner(){
  org.junit.Assume.assumeFalse(VlessProbeCoordinator.busy())
  val legacy=VlessProbeEngine{};legacy.start(run("old-remote",1)){JSONObject().put("outcome","success")};legacy.awaitStopped()
  val fields=listOf("engine","sequence","activeRun").map{VlessProbeCoordinator::class.java.getDeclaredField(it).apply{isAccessible=true}}
  val saved=fields.map{it.get(VlessProbeCoordinator)}
  try{
   fields[0].set(VlessProbeCoordinator,legacy);fields[1].set(VlessProbeCoordinator,null);fields[2].set(VlessProbeCoordinator,"new-local")
   assertFalse(VlessProbeCoordinator.cancel("old-remote"))
   assertEquals("new-local",fields[2].get(VlessProbeCoordinator))
  }finally{fields.forEachIndexed{i,field->field.set(VlessProbeCoordinator,saved[i])}}
 }
 private fun run(id:String,count:Int)=JSONObject().put("runId",id).put("sourceId","group").put("state","running").put("sequence",1).put("rows",JSONArray((0 until count).map{JSONObject().put("id","n$it").put("outcome","queued")}))
 @Test fun boundedWorkAndCancelIsolation(){
 val gate=CountDownLatch(1);val two=CountDownLatch(2);val active=AtomicInteger();val max=AtomicInteger();val finished=CountDownLatch(1)
 val engine=VlessProbeEngine{if(it.optString("state")=="finished")finished.countDown()}
 assertTrue(engine.start(run("one",8)){val n=active.incrementAndGet();max.updateAndGet{maxOf(it,n)};two.countDown();try{gate.await(3,TimeUnit.SECONDS);JSONObject().put("outcome","success")}finally{active.decrementAndGet()}})
 assertTrue(two.await(2,TimeUnit.SECONDS));assertFalse(engine.start(run("two",1)){JSONObject()});assertFalse(engine.cancel("wrong"));gate.countDown();assertTrue(finished.await(3,TimeUnit.SECONDS));assertEquals(2,max.get());assertEquals(8,engine.snapshot()!!.getJSONArray("rows").length())
 engine.awaitStopped();assertTrue(engine.start(run("two",1)){Thread.sleep(500);JSONObject().put("outcome","success")});assertFalse(engine.cancel("one"));assertTrue(engine.cancel("two"));assertEquals("canceled",engine.snapshot()!!.getString("state"))
 }
 @Test fun canceledWorkersDrainBeforeNewRun(){
 val entered=CountDownLatch(1);val release=CountDownLatch(1);val e=VlessProbeEngine{}
 assertTrue(e.start(run("old",1)){entered.countDown();while(release.count>0)try{release.await()}catch(_:InterruptedException){};JSONObject().put("outcome","success")})
 assertTrue(entered.await(2,TimeUnit.SECONDS));try{e.cancel("old");assertFalse(e.start(run("new",1)){JSONObject()})}finally{release.countDown()}
 }
 @Test fun rejectsTooManyNodes(){val e=VlessProbeEngine{};assertFalse(e.start(run("oversize",257)){JSONObject()})}
}
