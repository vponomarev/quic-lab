package ru.vpnc.quiclab
import org.json.JSONObject
import java.util.concurrent.Executors
import java.util.concurrent.ExecutorService
import java.util.concurrent.TimeUnit
/** Serial result transitions; workers never own the working VPN. */
internal class VlessProbeEngine(private val save:(JSONObject)->Unit){
 private var value:JSONObject?=null
 @Volatile private var pool:ExecutorService?=null
 fun awaitStopped(){pool?.awaitTermination(25,TimeUnit.SECONDS)}
 fun busy()=pool?.isTerminated==false
 private var generation=0L
 @Synchronized fun snapshot():JSONObject?=value?.let{JSONObject(it.toString())}
 @Synchronized fun start(run:JSONObject,work:(Int)->JSONObject):Boolean{
  val rows=run.getJSONArray("rows");if(rows.length() !in 1..256||(value?.optString("state")=="running"||busy()))return false
  generation++;val gen=generation;value=JSONObject(run.toString());publish()
  val workers=Executors.newFixedThreadPool(2);pool=workers;var remaining=rows.length()
  for(i in 0 until rows.length())workers.submit{
   synchronized(this){if(gen!=generation)return@submit;value!!.getJSONArray("rows").getJSONObject(i).put("outcome","running");publish()}
   val out=try{work(i)}catch(_:InterruptedException){JSONObject().put("outcome","canceled")}catch(_:Exception){JSONObject().put("outcome","transport")}
   synchronized(this){if(gen!=generation)return@submit;val row=value!!.getJSONArray("rows").getJSONObject(i);for(k in out.keys())row.put(k,out.get(k));row.put("stage","finished");remaining--
    if(remaining==0){value!!.put("state","finished").put("finishedAt",java.time.Instant.now().toString());workers.shutdown()};publish()}
  }
  workers.shutdown();return true
 }
 @Synchronized fun cancel(id:String,reason:String="canceled"):Boolean{
  val run=value?:return false;if(run.optString("runId")!=id||run.optString("state")!="running")return false
  generation++;pool?.shutdownNow();run.put("state",reason).put("finishedAt",java.time.Instant.now().toString())
  val rows=run.getJSONArray("rows");for(i in 0 until rows.length()){val row=rows.getJSONObject(i);if(row.optString("outcome") in listOf("queued","running"))row.put("outcome",if(reason in listOf("network_changed","configuration_changed","interrupted","budget","permission_denied"))reason else "canceled")};publish();return true
 }
 private fun publish(){val v=value?:return;v.put("sequence",v.optLong("sequence")+1);save(JSONObject(v.toString()))}
}
