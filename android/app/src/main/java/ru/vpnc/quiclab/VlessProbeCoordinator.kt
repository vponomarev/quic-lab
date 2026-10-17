package ru.vpnc.quiclab
import android.content.Context
import android.net.ConnectivityManager
import android.os.SystemClock
import org.json.JSONObject
import org.json.JSONArray
import java.util.UUID
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.ConcurrentHashMap
internal object VlessProbeCoordinator{
 private var app:Context?=null
 private var engine:VlessProbeEngine?=null
 private var store:VlessProbeStore?=null
 @Volatile private var lease:ProbeNetworkLease?=null
 private var timer:java.util.concurrent.ScheduledExecutorService?=null
 private val natives=ConcurrentHashMap<String,mobile.VLESSProbe>()
 @Volatile private var activeRun=""
 private var source:ProbeSource?=null
 @Volatile var foreground=false
 @Volatile private var started=0L
 @Synchronized private fun init(c:Context){if(app!=null)return;app=c.applicationContext;store=VlessProbeStore(app!!).also{it.recover()};engine=VlessProbeEngine{run->store!!.save(run);if(run.optString("state")!="running")cleanup()}}
 fun busy()=lease!=null
 fun results(c:Context):JSONObject{init(c);return store!!.all()}
 fun sources(c:Context)=VlessProbeSources.list(c)
 @Synchronized fun start(c:Context,id:String,action:String="start",previous:String="",initiator:String="local",runId:String=UUID.randomUUID().toString(),expected:String=""):String{
  init(c);if(lease!=null||engine!!.busy())return "busy"
  val prior=store!!.all();for(k in prior.keys())if(prior.getJSONObject(k).optString("runId")==runId)return "duplicate"
  val src=sources(c).firstOrNull{it.id==id}?:return "configuration_changed"
  if(expected.isNotEmpty()&&expected!=src.fingerprint)return "configuration_changed"
  if(initiator=="mdm" && !LabVpnService.active && !MdmService.foregroundReady)return "needs_foreground"
  if(initiator=="local" && !foreground && !LabVpnService.active && !MdmService.foregroundReady)return "needs_foreground"
  val old=prior.optJSONObject(id)
  val nodes=if(action=="retry"){
   if(old==null||old.optString("runId")!=previous||old.optString("fingerprint")!=src.fingerprint)return "configuration_changed"
   val rows=old.getJSONArray("rows");val failed=(0 until rows.length()).map{rows.getJSONObject(it)}.filter{it.optString("outcome")!="success"}.map{it.getString("id")}.toSet();src.nodes.filter{it.id in failed}
  }else src.nodes
  if(nodes.isEmpty())return "nothing_to_retry"
  val selected=try{VlessProbeNetwork.acquire(c)}catch(_:Exception){return "no_network"}
  lease=selected;activeRun=runId;source=src;started=SystemClock.elapsedRealtime()
  val localGateway=src.nodes.firstOrNull()?.gateway
  val localControl=localGateway?.optString("control_url").orEmpty()
  val origin=if(initiator=="local"&&localControl.startsWith("https://"))java.net.URI(localControl).let{"https://"+it.rawAuthority} else MdmStore(c).read().binding?.endpoint?:"https://quic-lab.vpnc.ru"
  val target=origin.trimEnd('/')+"/api/v1/probe/echo"
  val targetCA=if(initiator=="local"&&localControl.startsWith(origin))localGateway?.optString("ca").orEmpty()else ""
  val rows=JSONArray(nodes.map{n->n.publicRow()})
  val run=JSONObject().put("runId",runId).put("sourceId",src.id).put("fingerprint",src.fingerprint).put("sequence",0).put("network",selected.kind).put("initiator",initiator).put("state","running").put("startedAt",java.time.Instant.now().toString()).put("rows",rows)
  if(initiator=="mdm")MdmStore(c).read().let{run.put("ownerBinding",it.binding?.id).put("ownerEpoch",it.binding?.epoch).put("ownerGeneration",it.localGeneration)}
  timer=Executors.newSingleThreadScheduledExecutor().also{clock->clock.scheduleAtFixedRate({
   runCatching{
    val now=SystemClock.elapsedRealtime();val current=engine!!.snapshot()?:return@runCatching
    if(current.optString("state")!="running")return@runCatching
    val reason=when{
     now-started>45*60*1000L->"interrupted"
     VlessProbePolicy.networkChanged(selected.network.networkHandle,VlessProbeNetwork.selected(c)?.networkHandle,c.getSystemService(ConnectivityManager::class.java).getNetworkCapabilities(selected.network)!=null)->"network_changed"
     initiator=="mdm" && !VlessProbePolicy.owns(run,MdmStore(c).read())->"permission_denied"
     !foreground && !LabVpnService.active && !MdmService.foregroundReady->"interrupted"
     sources(c).firstOrNull{it.id==src.id}?.fingerprint!=src.fingerprint->"configuration_changed"
     else->""
    };if(reason.isNotEmpty())cancel(runId,reason)
   }
  },1,2,TimeUnit.SECONDS)}
  engine!!.start(run){index->
   val n=nodes[index];val deadline=SystemClock.elapsedRealtime()+20000
   var native:mobile.VLESSProbe?=null
   var alarm:java.util.concurrent.ScheduledFuture<*>?=null
   try{
    if(selected.kind=="cell"&&JSONObject(selected.budget.snapshot()).optBoolean("blocked"))return@start JSONObject().put("outcome","budget")
    val config=JSONObject(n.config.toString());val host=n.endpoint.substringBeforeLast(':').removeSurrounding("[","]");val port=n.endpoint.substringAfterLast(':')
    val ip=selected.resolve(host,deadline);if(Thread.currentThread().isInterrupted)throw InterruptedException()
    config.put("Endpoint","$ip:$port")
    native=mobile.Mobile.newVLESSProbe(config.toString(),selected.binder,selected.budget,selected.kind);natives["$runId:$index"]=native!!
    if(activeRun!=runId||Thread.currentThread().isInterrupted){native!!.close();throw InterruptedException()}
    alarm=timer?.schedule({native?.close()},(deadline-SystemClock.elapsedRealtime()).coerceAtLeast(1),TimeUnit.MILLISECONDS)
    val options=JSONObject().put("target",target);if(targetCA.isNotEmpty())options.put("ca",targetCA);n.gateway?.let{options.put("gateway",it)}
    val result=JSONObject(native!!.run(options.toString()))
    if(SystemClock.elapsedRealtime()>=deadline)result.put("outcome","timeout")
    VlessProbePolicy.normalize(result,selected.kind=="cell",JSONObject(selected.budget.snapshot()).optBoolean("blocked")).put("measuredAt",java.time.Instant.now().toString())
   }catch(_:java.net.UnknownHostException){JSONObject().put("outcome","dns")}catch(_:java.net.SocketTimeoutException){JSONObject().put("outcome","timeout")}catch(_:InterruptedException){JSONObject().put("outcome","canceled")}catch(_:Exception){JSONObject().put("outcome",if(selected.kind=="cell"&&JSONObject(selected.budget.snapshot()).optBoolean("blocked"))"budget" else "transport")}
   finally{alarm?.cancel(false);native?.close();natives.remove("$runId:$index");runCatching{VpnBudgetRun.sharedForContext(c).checkpoint()}}
  }
  return "queued"
 }
 fun cancel(id:String,reason:String="canceled"):Boolean{val current=engine?.snapshot();if(current?.optString("runId")!=id)return false;activeRun="";for(n in natives.values)runCatching{n.close()};return engine?.cancel(id,reason)?:false}
 private fun cleanup(){activeRun="";timer?.shutdownNow();timer=null;for(n in natives.values)runCatching{n.close()};val old=lease
  Thread({engine?.awaitStopped();runCatching{old?.close()};if(lease===old)lease=null},"probe-cleanup").start()
 }
}
