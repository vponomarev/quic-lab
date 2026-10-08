package ru.vpnc.quiclab

import android.content.Context
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.time.Instant
import java.util.concurrent.ConcurrentHashMap

/** Encrypted hourly buckets, separate from credentials and the control/event queue. */
internal class MdmTelemetryStore(private val dir:File,private val maxBytes:Long=50L*1024*1024){
 constructor(c:Context):this(File(c.noBackupFilesDir,"mdm-radio"))
 private val lock=locks.computeIfAbsent(dir.absolutePath){Any()}
 private fun storage(f:File)=MdmStore(f,"quic-lab-mdm-radio")
 private fun files()=dir.listFiles()?.filter{it.name.matches(Regex("[0-9]+\\.bin"))}?.sortedBy{it.name.substringBefore('.').toLong()}?:emptyList()
 private fun rows(f:File)=storage(f).document().optJSONArray("records")?:JSONArray()
 private fun save(f:File,a:JSONArray){
  if(a.length()==0){f.delete();return}
  storage(f).edit{it.put("records",a)}
 }
 private fun meta()=storage(File(dir,"meta.bin"))
 private fun prune(now:Long){
  dir.mkdirs()
  var discarded=0L
  files().forEach{f->
   val a=rows(f);val kept=JSONArray()
   for(i in 0 until a.length()){
    val r=a.getJSONObject(i)
    val at=Instant.parse(r.getString("measuredAt")).toEpochMilli()
    if(at<now-24*3600_000L)discarded++ else kept.put(r)
   }
   if(kept.length()!=a.length())save(f,kept)
  }
  var total=files().sumOf{it.length()}
  for(f in files()){
   if(total<=maxBytes)break
   total-=f.length();discarded+=rows(f).length();check(f.delete())
  }
  if(discarded>0)meta().edit{it.put("dropped",Math.addExact(it.optLong("dropped"),discarded))}
 }
 fun append(sample:JSONObject,now:Long)=synchronized(lock){
  require(sample.toString().toByteArray().size<=8192)
  val at=Instant.parse(sample.getString("measuredAt")).toEpochMilli()
  require(at in (now-24*3600_000L)..(now+300_000))
  require(sample.getString("id").matches(Regex("[a-zA-Z0-9-]{1,80}")))
  prune(now)
  val f=File(dir,"${at/3600_000}.bin");val a=rows(f)
  if((0 until a.length()).none{a.getJSONObject(it).getString("id")==sample.getString("id")}){
   require(a.length()<180){"Слишком частый сбор телеметрии"}
   a.put(sample);save(f,a);prune(now)
  }
 }
 fun pending(now:Long):JSONArray=synchronized(lock){
  prune(now);val out=JSONArray()
  for(f in files()){
   val a=rows(f)
   for(i in 0 until a.length()){if(out.length()==64)return@synchronized out;out.put(a.getJSONObject(i))}
  }
  out
 }
 fun acknowledge(ids:Set<String>,now:Long)=synchronized(lock){
  for(f in files()){
   val a=rows(f);val kept=JSONArray()
   for(i in 0 until a.length()){val r=a.getJSONObject(i);if(r.getString("id") !in ids)kept.put(r)}
   if(a.length()!=kept.length())save(f,kept)
  }
  prune(now)
 }
 fun dropped():Long=synchronized(lock){if(!dir.exists())0L else meta().document().optLong("dropped")}
 fun erase()=synchronized(lock){if(dir.exists())check(dir.deleteRecursively())}
 companion object{private val locks=ConcurrentHashMap<String,Any>()}
}
