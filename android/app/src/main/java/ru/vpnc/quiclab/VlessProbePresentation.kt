package ru.vpnc.quiclab

import org.json.JSONObject

internal enum class ProbeHealth { LIVE, FAILED, UNKNOWN, NOT_APPLICABLE }
internal data class ProbeAvailability(val vless:ProbeHealth,val ha:ProbeHealth)
internal object VlessProbePresentation {
 private val neutral=setOf("","queued","running","pending","canceled","interrupted","network_changed","configuration_changed","permission_denied","no_network","budget","skipped","not_attempted","unknown")
 fun classify(row:JSONObject):ProbeAvailability {
  val outcome=row.optString("outcome")
  val vless=when {
   row.optString("httpsOutcome")=="success" || outcome in setOf("success","relay_failed")->ProbeHealth.LIVE
   outcome in neutral->ProbeHealth.UNKNOWN
   else->ProbeHealth.FAILED
  }
  val relay=row.optString("relayOutcome")
  val ha=when {
   relay=="success"->ProbeHealth.LIVE
   relay=="not_applicable"->if(row.optBoolean("haConfigured"))ProbeHealth.UNKNOWN else ProbeHealth.NOT_APPLICABLE
   relay in neutral->ProbeHealth.UNKNOWN
   else->ProbeHealth.FAILED
  }
  return ProbeAvailability(vless,ha)
 }
 fun networks(run:JSONObject):List<String> {
  val phases=run.optJSONObject("phases")
  return if(phases!=null)listOf("wifi","cell").filter{phases.has(it)} else listOf(run.optString("network")).filter{it.isNotBlank()}
 }
 fun phase(run:JSONObject,network:String):JSONObject? = run.optJSONObject("phases")?.optJSONObject(network)
  ?:run.takeIf{!it.has("phases")&&it.optString("network")==network}
 fun rows(run:JSONObject,network:String):List<JSONObject> {
  val rows=phase(run,network)?.optJSONArray("rows")?:return emptyList()
  return (0 until rows.length()).mapNotNull{rows.optJSONObject(it)}
 }
 fun counts(rows:List<JSONObject>):List<Int> = listOf(ProbeHealth.LIVE,ProbeHealth.FAILED,ProbeHealth.UNKNOWN).map{state->rows.count{classify(it).vless==state}}
 fun filter(rows:List<JSONObject>,filter:Int):List<JSONObject> = rows.filter{
  when(filter){1->classify(it).vless==ProbeHealth.LIVE;2->classify(it).vless==ProbeHealth.FAILED;3->classify(it).vless==ProbeHealth.UNKNOWN;else->true}
 }
}
