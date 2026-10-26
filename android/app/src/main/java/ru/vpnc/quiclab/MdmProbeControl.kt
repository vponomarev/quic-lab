package ru.vpnc.quiclab
import android.content.Context
import org.json.JSONObject
import java.time.Instant
internal object MdmProbeControl{
 fun execute(c:Context,state:MdmState,command:MdmCommand,now:Instant):String{
  val store=MdmStore(c);val latest=store.read()
  val error=when{
   !latest.active||latest.cleanupPending||!latest.rights.vpn||!latest.rights.telemetry||latest.localGeneration!=state.localGeneration->"permission_denied"
   latest.binding?.id!=command.bindingId||latest.binding?.epoch!=command.epoch->"stale_epoch"
   !now.isBefore(command.expiresAt)||command.issuedAt.isAfter(now.plusSeconds(30))->"expired"
   !store.document().optBoolean("server_vless_probe_v1")->"unsupported"
   else->""
  }
  fun record(result:String):String{MdmVpnControl.event(c,"command_result",result,commandID=command.id);return result}
  if(error.isNotEmpty())return record(error)
  val prior=store.document().optJSONObject("probes_done")?.optJSONObject(command.id)
  if(prior!=null)return record(prior.optString("result","interrupted"))
  val p=command.probe?:return record("invalid")
  val action=p.optString("action");if(action !in listOf("start","retry","cancel"))return record("invalid")
  store.edit{j->val done=j.optJSONObject("probes_done")?:JSONObject();done.keys().asSequence().toList().forEach{if(done.getJSONObject(it).optLong("until")<now.epochSecond)done.remove(it)};check(done.length()<1000);done.put(command.id,JSONObject().put("result","interrupted").put("until",command.expiresAt.plusSeconds(86400).epochSecond));j.put("probes_done",done)}
  val result=try{if(action=="cancel"){if(VlessProbeCoordinator.cancel(p.optString("runId")))"canceled" else "not_running"}else VlessProbeCoordinator.start(c,p.getString("sourceId"),action,p.optString("runId"),"mdm",command.id,p.getString("fingerprint"))}catch(_:Exception){"probe_failed"}
  store.edit{it.getJSONObject("probes_done").getJSONObject(command.id).put("result",result)}
  return record(result)
 }
 fun report(c:Context,state:MdmState,out:JSONObject){
  if(!state.rights.vpn||!state.rights.telemetry||!MdmStore(c).document().optBoolean("server_vless_probe_v1"))return
  val sources=VlessProbeCoordinator.sources(c);out.put("probeSources",org.json.JSONArray(sources.map{it.publicJSON()}))
  val all=VlessProbeCoordinator.results(c)
  val run=all.keys().asSequence().map{all.getJSONObject(it)}.filter{r->VlessProbePolicy.reportableV1(r)&&VlessProbePolicy.owns(r,state)&&sources.any{it.id==r.optString("sourceId")&&it.fingerprint==r.optString("fingerprint")}}.maxByOrNull{it.optString("startedAt")}
  if(run!=null){val wire=JSONObject(run.toString());for(k in listOf("ownerBinding","ownerEpoch","ownerGeneration"))wire.remove(k);out.put("probeSnapshots",org.json.JSONArray().put(wire))}
 }
}
