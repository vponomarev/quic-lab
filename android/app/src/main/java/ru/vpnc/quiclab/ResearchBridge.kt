package ru.vpnc.quiclab

import android.content.Context
import mobile.ResearchObserver

internal object ResearchBridge {
 fun flowOwner(context:Context)=object:mobile.FlowOwner {
  private val cm=context.getSystemService(android.net.ConnectivityManager::class.java)
  override fun owner(protocol:Long,local:String,localPort:Long,remote:String,remotePort:Long):Long=runCatching{
   cm.getConnectionOwnerUid(protocol.toInt(),java.net.InetSocketAddress(local,localPort.toInt()),java.net.InetSocketAddress(remote,remotePort.toInt())).toLong()
  }.getOrDefault(-1L)
 }
 fun scopeJSON(context:Context):String {
  val s=ResearchSettings.effectiveScope(context)
  val uids=s.packages.mapNotNull{runCatching{context.packageManager.getApplicationInfo(it,0).uid}.getOrNull()}.distinct()
  return org.json.JSONObject().put("mode",s.mode).put("direct_allowed",runCatching{ResearchSettings.directAllowed(MdmStore(context).read())}.getOrDefault(false))
   .put("uids",org.json.JSONArray(uids)).toString()
 }
 fun rule(context:Context,id:String,mode:Int,apps:Set<String>,routes:String):org.json.JSONObject {
  val rule=org.json.JSONObject().put("id",id).put("mode",when(mode){1->"apps";2->"exclude";3->"subnets";else->"all"})
  if(mode==1 || mode==2)rule.put("uids",org.json.JSONArray(apps.mapNotNull{runCatching{context.packageManager.getApplicationInfo(it,0).uid}.getOrNull()}.distinct()))
  if(mode==3)rule.put("subnets",org.json.JSONArray(VpnRoutes.parse(routes).map{"${it.first}/${it.second}"}))
  return rule
 }

 fun observer(context:Context,direct:Boolean=false):ResearchObserver {
  val app=context.applicationContext
  val owners=ResearchOwnerLookup(app)
  return object:ResearchObserver {
   override fun allowed()=if(direct)ResearchSettings.effectiveScope(app).mode!="vpn_only" else ResearchSettings.allowed(app)
   override fun ownerForUID(uid:Long):String=if(allowed() && uid in 0..Int.MAX_VALUE.toLong())owners.lookupUID(uid.toInt())else ""
   override fun owner(protocol:Long,local:String,localPort:Long,remote:String,remotePort:Long):String {
    if(!allowed())return ""
    if(protocol !in 0L..255L || localPort !in 1L..65535L || remotePort !in 1L..65535L)return ""
    return owners.lookup(protocol.toInt(),local,localPort.toInt(),remote,remotePort.toInt())
   }
  }
 }
}
