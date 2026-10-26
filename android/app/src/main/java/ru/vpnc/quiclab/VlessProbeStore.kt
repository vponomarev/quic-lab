package ru.vpnc.quiclab
import android.content.Context
import android.util.AtomicFile
import org.json.JSONObject
import java.io.File
internal class VlessProbeStore(c:Context){
 private val file=AtomicFile(File(c.filesDir,"vless-probe-results.json"))
 @Synchronized fun all():JSONObject=try{file.openRead().use{JSONObject(it.readBytes().toString(Charsets.UTF_8))}}catch(_:java.io.FileNotFoundException){JSONObject()}
 @Synchronized fun save(run:JSONObject){
  val phases=run.optJSONObject("phases")
  if(run.optInt("schema",1)==2){
   require(phases!=null&&phases.length() in 1..2)
   for(k in phases.keys())require(phases.getJSONObject(k).toString().toByteArray().size<=512*1024)
   require(run.toString().toByteArray().size<=1536*1024)
  }else require(run.toString().toByteArray().size<=512*1024)
  val all=all();all.put(run.getString("sourceId"),JSONObject(run.toString()));while(all.length()>32){val oldest=all.keys().asSequence().minByOrNull{all.getJSONObject(it).optString("startedAt")}!!;all.remove(oldest)}
  val out=file.startWrite();try{out.write(all.toString().toByteArray());file.finishWrite(out)}catch(e:Exception){file.failWrite(out);throw e}
 }
 @Synchronized fun recover(){val all=all();for(k in all.keys()){
  val run=all.getJSONObject(k);if(run.optString("state")=="running"){
   run.put("state","interrupted").put("sequence",run.optLong("sequence")+1).put("finishedAt",java.time.Instant.now().toString())
   interruptRows(run)
   run.optJSONObject("phases")?.let{phases->for(kind in phases.keys()){
    val phase=phases.getJSONObject(kind)
    if(phase.optString("state") in listOf("queued","running")){phase.put("state","interrupted");interruptRows(phase)}
   }};save(run)
  }
 }}
 private fun interruptRows(value:JSONObject){val rows=value.optJSONArray("rows")?:return;for(i in 0 until rows.length()){val row=rows.getJSONObject(i);if(row.optString("outcome") in listOf("running","queued"))row.put("outcome","interrupted")}}
}
