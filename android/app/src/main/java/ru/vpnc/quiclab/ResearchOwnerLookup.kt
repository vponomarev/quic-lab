package ru.vpnc.quiclab

import android.content.Context
import android.net.ConnectivityManager
import android.net.InetAddresses
import android.os.SystemClock
import org.json.JSONArray
import org.json.JSONObject
import java.net.InetSocketAddress

/** Bounded dictionary lookup; failures never prevent forwarding or guess a package owner. */
internal class ResearchOwnerLookup(context:Context) {
 private val cm=context.getSystemService(ConnectivityManager::class.java)
 private val pm=context.packageManager
 private data class Entry(val at:Long,val json:String)
 private val cache=object:LinkedHashMap<Int,Entry>(32,0.75f,true){
  override fun removeEldestEntry(eldest:MutableMap.MutableEntry<Int,Entry>?)=size>256
 }
 fun lookup(protocol:Int,local:String,localPort:Int,remote:String,remotePort:Int):String {
  if(protocol!=6 && protocol!=17 || localPort !in 1..65535 || remotePort !in 1..65535)return unknown()
  return try{
   val uid=cm.getConnectionOwnerUid(protocol,
    InetSocketAddress(InetAddresses.parseNumericAddress(local),localPort),
    InetSocketAddress(InetAddresses.parseNumericAddress(remote),remotePort))
   lookupUID(uid)
  }catch(_:Exception){unknown()}
 }
 fun lookupUID(uid:Int):String {
  if(uid<0)return unknown()
  return try {
   val now=SystemClock.elapsedRealtime()
   synchronized(cache){cache[uid]?.takeIf{now-it.at<30_000}?.let{return it.json}}
   val packages=pm.getPackagesForUid(uid)?.sorted()?.take(32).orEmpty()
   val labels=packages.map { name->runCatching{pm.getApplicationLabel(pm.getApplicationInfo(name,0)).toString()}.getOrDefault(name) }
   val json=identity(uid,packages,labels)
   synchronized(cache){cache[uid]=Entry(now,json)}
   json
  }catch(_:Exception){unknown()}
 }
 companion object {
  private fun unknown()=identity(-1,emptyList(),emptyList())
  internal fun identity(uid:Int,packages:List<String>,labels:List<String>):String {
   val names=packages.take(32).map{it.take(200)}
   return JSONObject().put("id",if(uid<0)"0" else (uid.toLong()+1).toString())
    .put("uid",uid.toString()).put("packages",JSONArray(names))
    .put("labels",JSONArray(labels.take(names.size).map{it.take(200)}))
    .put("attribution",when{uid<0 || names.isEmpty()->"unknown";packages.size>1->"shared_uid";else->"known"}).toString()
  }
 }
}
