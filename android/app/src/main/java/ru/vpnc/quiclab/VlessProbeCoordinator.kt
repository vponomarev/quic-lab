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
 private var sequence:VlessProbeSequence?=null
 private var store:VlessProbeStore?=null
 @Volatile private var lease:ProbeNetworkLease?=null
 private var timer:java.util.concurrent.ScheduledExecutorService?=null
 private val natives=ConcurrentHashMap<String,mobile.VLESSProbe>()
 @Volatile private var activeRun=""
 private var source:ProbeSource?=null
 @Volatile var foreground=false
 @Volatile private var started=0L
 @Synchronized private fun init(c:Context){if(app!=null)return;app=c.applicationContext;store=VlessProbeStore(app!!).also{it.recover()};engine=VlessProbeEngine{run->store!!.save(run);if(run.optString("state")!="running")cleanup()}}
 fun busy()=lease!=null||sequence?.busy()==true
 fun results(c:Context):JSONObject{init(c);return store!!.all()}
 fun sources(c:Context)=VlessProbeSources.list(c)
 @Synchronized fun start(c:Context,id:String,action:String="start",previous:String="",initiator:String="local",runId:String=UUID.randomUUID().toString(),expected:String=""):String{
  init(c);if(busy()||engine!!.busy())return "busy"
  val prior=store!!.all();for(k in prior.keys())if(prior.getJSONObject(k).optString("runId")==runId)return "duplicate"
  val listed=sources(c)
  val src=findProbeSource(listed,id)
   ?:return "configuration_changed"
  if(src.nodes.size>256)return "too_many_nodes"
  if(expected.isNotEmpty()&&expected!=src.fingerprint)return "configuration_changed"
  if(initiator=="mdm" && !LabVpnService.active && !MdmService.foregroundReady)return "needs_foreground"
  if(initiator=="local" && !foreground && !LabVpnService.active && !MdmService.foregroundReady)return "needs_foreground"
  val old=prior.optJSONObject(id)
  val nodes=if(action=="retry"){
   if(old==null||old.optString("runId")!=previous||old.optString("fingerprint")!=src.fingerprint)return "configuration_changed"
   val failed=probeRetryIds(old);src.nodes.filter{it.id in failed}
  }else src.nodes
  if(nodes.isEmpty())return "nothing_to_retry"
  if(initiator=="local")return startLocal(c,src,nodes,runId)
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
     findProbeSource(sources(c),src.id)?.fingerprint!=src.fingerprint->"configuration_changed"
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
    config.put("Endpoint",if(ip.contains(':'))"[$ip]:$port" else "$ip:$port")
    native=mobile.Mobile.newVLESSProbe(config.toString(),selected.binder,selected.budget,selected.kind);natives["$runId:$index"]=native!!
    if(activeRun!=runId||Thread.currentThread().isInterrupted){native!!.close();throw InterruptedException()}
    alarm=timer?.schedule({native?.close()},(deadline-SystemClock.elapsedRealtime()).coerceAtLeast(1),TimeUnit.MILLISECONDS)
    val options=JSONObject().put("target",target);if(targetCA.isNotEmpty())options.put("ca",targetCA);n.gateway?.let{options.put("gateway",it)}
    val result=JSONObject(native!!.run(options.toString()))
    if(SystemClock.elapsedRealtime()>=deadline)result.put("outcome","timeout")
    VlessProbePolicy.normalize(result,selected.kind=="cell",JSONObject(selected.budget.snapshot()).optBoolean("blocked")).put("measuredAt",java.time.Instant.now().toString())
   }catch(_:java.net.UnknownHostException){JSONObject().put("outcome","dns").put("dnsOutcome","dns").put("tcpOutcome","not_attempted").put("httpsOutcome","not_attempted").put("exitIpOutcome","not_attempted").put("measuredAt",java.time.Instant.now().toString())}catch(_:java.net.SocketTimeoutException){JSONObject().put("outcome","timeout")}catch(_:InterruptedException){JSONObject().put("outcome","canceled")}catch(_:Exception){JSONObject().put("outcome",if(selected.kind=="cell"&&JSONObject(selected.budget.snapshot()).optBoolean("blocked"))"budget" else "transport")}
   finally{alarm?.cancel(false);native?.close();natives.remove("$runId:$index");runCatching{VpnBudgetRun.sharedForContext(c).checkpoint()}}
  }
  return "queued"
 }
 @Synchronized fun cancel(id:String,reason:String="canceled"):Boolean{
  if(sequence?.snapshot()?.optString("runId")==id&&sequence?.active()==true){activeRun="";val closing=natives.values.toList();val changed=sequence!!.cancel(id,reason);for(n in closing)runCatching{n.close()};return changed}
  val current=engine?.snapshot();if(current?.optString("runId")!=id||current.optString("state")!="running"||activeRun!=id)return false
  val closing=natives.values.toList();val changed=engine?.cancel(id,reason)?:false
  if(changed){activeRun="";for(n in closing)runCatching{n.close()}};return changed
 }
 private fun startLocal(c:Context,src:ProbeSource,nodes:List<ProbeNode>,runId:String):String {
  val first=VlessProbeNetwork.selectedKind(c)?:return "no_network"
  val run=JSONObject().put("runId",runId).put("sourceId",src.id).put("fingerprint",src.fingerprint).put("initiator","local").put("startedAt",java.time.Instant.now().toString()).put("rows",JSONArray(nodes.map{it.publicRow()}))
  activeRun=runId;started=SystemClock.elapsedRealtime()
  val serial=VlessProbeSequence{result->
   store!!.save(result)
   if(result.optString("state")!="running"){activeRun="";timer?.shutdownNow();timer=null}
  };sequence=serial
  fun block(kind:String,meter:mobile.TrafficBudget?=null):String {
   val reserve=VpnReserveSettings.allowed(c,android.net.NetworkCapabilities.TRANSPORT_CELLULAR,false)
   return VlessProbePolicy.phaseBlockReason(kind,first,reserve,meter?.let{JSONObject(it.snapshot()).optBoolean("blocked")}?:false)
  }
  timer=Executors.newSingleThreadScheduledExecutor().also{clock->clock.scheduleAtFixedRate({
   runCatching{
    if(!serial.active())return@runCatching
    val reason=when{
     SystemClock.elapsedRealtime()-started>45*60*1000L->"interrupted"
     !foreground&&!LabVpnService.active&&!MdmService.foregroundReady->"interrupted"
     findProbeSource(sources(c),src.id)?.fingerprint!=src.fingerprint->"configuration_changed"
     else->""
    }
    if(reason.isNotEmpty())cancel(runId,reason)
    else lease?.let{selected->
     val phaseReason=block(selected.kind,selected.budget).ifEmpty{if(VlessProbeNetwork.available(c,selected.network,selected.kind))"" else "network_changed"}
     if(phaseReason.isNotEmpty()){val closing=natives.values.toList();if(serial.cancelPhase(phaseReason,selected.kind))for(n in closing)runCatching{n.close()}}
    }
   }
  },1,1,TimeUnit.SECONDS)}
  serial.start(run,VlessProbePolicy.phasePlan(first)){kind->
   block(kind).takeIf{it.isNotEmpty()}?.let{throw ProbePhaseSkipped(it)}
   val selected=try{VlessProbeNetwork.acquire(c,kind){
    if(!serial.active())true else {block(kind).takeIf{it.isNotEmpty()}?.let{throw ProbePhaseSkipped(it)};false}
   }}catch(e:ProbePhaseSkipped){throw e}catch(e:Exception){throw ProbePhaseSkipped(if(e.message=="budget")"budget" else "no_network")}
   lease=selected
   ProbePhase({index->
    val denied=block(kind,selected.budget).ifEmpty{if(VlessProbeNetwork.available(c,selected.network,kind))"" else "network_changed"}
    if(denied.isNotEmpty())JSONObject().put("outcome",denied)
    else localProbe(c,runId,nodes[index],selected,index)
   },{try{selected.close()}finally{if(lease===selected)lease=null}})
  }
  return "queued"
 }
 private fun localProbe(c:Context,runId:String,n:ProbeNode,selected:ProbeNetworkLease,index:Int):JSONObject {
  val deadline=SystemClock.elapsedRealtime()+20000
  var native:mobile.VLESSProbe?=null
  var alarm:java.util.concurrent.ScheduledFuture<*>?=null
  try {
   if(activeRun!=runId)throw InterruptedException()
   val config=JSONObject(n.config.toString());val host=n.endpoint.substringBeforeLast(':').removeSurrounding("[","]");val port=n.endpoint.substringAfterLast(':')
   val ip=selected.resolve(host,deadline);if(Thread.currentThread().isInterrupted)throw InterruptedException()
   config.put("Endpoint",if(ip.contains(':'))"[$ip]:$port" else "$ip:$port")
   native=mobile.Mobile.newVLESSProbe(config.toString(),selected.binder,selected.budget,selected.kind);natives["$runId:$index"]=native
   if(activeRun!=runId||Thread.currentThread().isInterrupted)throw InterruptedException()
   alarm=timer?.schedule({native?.close()},(deadline-SystemClock.elapsedRealtime()).coerceAtLeast(1),TimeUnit.MILLISECONDS)
   val control=n.gateway?.optString("control_url").orEmpty()
   val origin=if(control.startsWith("https://"))java.net.URI(control).let{"https://"+it.rawAuthority}else MdmStore(c).read().binding?.endpoint?:"https://quic-lab.vpnc.ru"
   val options=JSONObject().put("target",origin.trimEnd('/')+"/api/v1/probe/echo")
   if(control.startsWith("https://"))options.put("ca",n.gateway?.optString("ca").orEmpty())
   n.gateway?.let{options.put("gateway",it)}
   val result=JSONObject(native.run(options.toString()))
   if(SystemClock.elapsedRealtime()>=deadline)result.put("outcome","timeout")
   return VlessProbePolicy.normalize(result,selected.kind=="cell",JSONObject(selected.budget.snapshot()).optBoolean("blocked")).put("measuredAt",java.time.Instant.now().toString())
  }catch(_:java.net.UnknownHostException){return JSONObject().put("outcome","dns").put("dnsOutcome","dns")}
   catch(_:java.net.SocketTimeoutException){return JSONObject().put("outcome","timeout")}
   catch(_:InterruptedException){return JSONObject().put("outcome","canceled")}
   catch(_:Exception){return JSONObject().put("outcome",if(selected.kind=="cell"&&JSONObject(selected.budget.snapshot()).optBoolean("blocked"))"budget" else "transport")}
  finally{alarm?.cancel(false);native?.close();natives.remove("$runId:$index");runCatching{VpnBudgetRun.sharedForContext(c).checkpoint()}}
 }
 private fun cleanup(){activeRun="";timer?.shutdownNow();timer=null;for(n in natives.values)runCatching{n.close()};val old=lease
  Thread({engine?.awaitStopped();runCatching{old?.close()};if(lease===old)lease=null},"probe-cleanup").start()
 }
}
