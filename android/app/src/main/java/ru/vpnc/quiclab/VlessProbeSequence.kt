package ru.vpnc.quiclab

import org.json.JSONObject

internal class ProbePhase(val work:(Int)->JSONObject,private val release:()->Unit):AutoCloseable {
 override fun close()=release()
}

internal class VlessProbeSequence(private val save:(JSONObject)->Unit) {
 private var value:JSONObject?=null
 @Volatile private var running=false
 @Volatile private var engine:VlessProbeEngine?=null
 fun busy()=running
 @Synchronized fun snapshot():JSONObject?=value?.let{JSONObject(it.toString())}
 @Synchronized fun active()=value?.optString("state")=="running"
 @Synchronized fun start(run:JSONObject,kinds:List<String>,acquire:(String)->ProbePhase):Boolean {
  if(running||kinds.isEmpty()||kinds.size>2||kinds.distinct().size!=kinds.size||run.getJSONArray("rows").length() !in 1..256)return false
  value=JSONObject(run.toString()).put("schema",2).put("state","running").put("sequence",0)
  val phases=JSONObject()
  for(kind in kinds)phases.put(kind,JSONObject().put("network",kind).put("state","queued").put("rows",org.json.JSONArray(run.getJSONArray("rows").toString())))
  value!!.put("phases",phases);running=true;publish()
  Thread({
   try {
    for(kind in kinds) {
     if(!active())break
     var phase:ProbePhase?=null
     try {
      phase=acquire(kind)
      if(!active())break
      val phaseRun=synchronized(this){JSONObject(run.toString()).put("network",kind).put("state","running").put("rows",org.json.JSONArray(phases.getJSONObject(kind).getJSONArray("rows").toString()))}
      val current=VlessProbeEngine{result->synchronized(this){if(active()){
       phases.put(kind,JSONObject(result.toString()));value!!.put("network",kind).put("rows",result.getJSONArray("rows"));publish()
      }}}
      engine=current
      current.start(phaseRun,phase.work)
      if(!active())current.cancel(run.getString("runId"))
      // Drain even workers which take time to honor cancellation before releasing the lease.
      while(current.busy())current.awaitStopped()
     } catch(e:Exception) {
      synchronized(this){if(active()){
       val reason=(e as? ProbePhaseSkipped)?.reason?:"no_network"
       val result=phases.getJSONObject(kind).put("state","skipped").put("reason",reason)
       val rows=result.getJSONArray("rows");for(i in 0 until rows.length())rows.getJSONObject(i).put("outcome",reason)
       value!!.put("network",kind).put("rows",rows);publish()
      }}
     } finally {engine=null;runCatching{phase?.close()}}
    }
   } finally {synchronized(this){try{if(active()){value!!.put("state","finished").put("finishedAt",java.time.Instant.now().toString());publish()}}finally{running=false}}}
  },"probe-sequence").start()
  return true
 }
 fun cancel(id:String,reason:String="canceled"):Boolean {
  val current=synchronized(this){
   val run=value?:return false
   if(run.optString("runId")!=id||!active())return false
   run.put("state",reason).put("finishedAt",java.time.Instant.now().toString())
   val phases=run.getJSONObject("phases")
   for(k in phases.keys()){
    val phase=phases.getJSONObject(k)
    if(phase.optString("state") in listOf("queued","running")){
     phase.put("state",reason)
     val rows=phase.getJSONArray("rows");for(i in 0 until rows.length())if(rows.getJSONObject(i).optString("outcome") in listOf("queued","running"))rows.getJSONObject(i).put("outcome",reason)
    }
   };phases.optJSONObject(run.optString("network"))?.let{run.put("rows",it.getJSONArray("rows"))};publish();engine
  }
  current?.cancel(id,reason);return true
 }
 fun cancelPhase(reason:String,expectedKind:String?=null):Boolean {
  val current=engine?:return false
  val run=current.snapshot()?:return false
  if(expectedKind!=null&&run.optString("network")!=expectedKind)return false
  return current.cancel(run.optString("runId"),reason)
 }
 private fun publish(){value!!.put("sequence",value!!.optLong("sequence")+1);save(JSONObject(value.toString()))}
}

internal class ProbePhaseSkipped(val reason:String):Exception(reason)

internal fun probeRetryIds(old:JSONObject):Set<String> {
 val phases=old.optJSONObject("phases")
 val snapshots=if(phases==null)listOf(old)else phases.keys().asSequence().map{phases.getJSONObject(it)}.toList()
 return snapshots.flatMap{phase->val rows=phase.getJSONArray("rows");(0 until rows.length()).map{rows.getJSONObject(it)}}.filter{it.optString("outcome")!="success"}.map{it.getString("id")}.toSet()
}
