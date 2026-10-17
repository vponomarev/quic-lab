package ru.vpnc.quiclab
import android.content.Context
import android.util.AtomicFile
import org.json.JSONObject
import java.io.File
internal class VlessProbeStore(c:Context){
 private val file=AtomicFile(File(c.filesDir,"vless-probe-results.json"))
 @Synchronized fun all():JSONObject=try{file.openRead().use{JSONObject(it.readBytes().toString(Charsets.UTF_8))}}catch(_:java.io.FileNotFoundException){JSONObject()}
 @Synchronized fun save(run:JSONObject){val all=all();all.put(run.getString("sourceId"),JSONObject(run.toString()));while(all.length()>32){val oldest=all.keys().asSequence().minByOrNull{all.getJSONObject(it).optString("startedAt")}!!;all.remove(oldest)};require(run.toString().toByteArray().size<=512*1024);val out=file.startWrite();try{out.write(all.toString().toByteArray());file.finishWrite(out)}catch(e:Exception){file.failWrite(out);throw e}}
 @Synchronized fun recover(){val all=all();for(k in all.keys()){val run=all.getJSONObject(k);if(run.optString("state")=="running"){run.put("state","interrupted").put("sequence",run.optLong("sequence")+1).put("finishedAt",java.time.Instant.now().toString());val rows=run.getJSONArray("rows");for(i in 0 until rows.length()){val row=rows.getJSONObject(i);if(row.optString("outcome") in listOf("running","queued"))row.put("outcome","interrupted")};save(run)}}}
}
