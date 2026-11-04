package ru.vpnc.quiclab

import org.json.JSONObject

internal object MdmProbeProtocol {
 const val CAPABILITY="vless_probe_sequence_v2"
 const val SERVER_FLAG="server_vless_probe_sequence_v2"
 const val KIND="vless_probe_sequence"
 fun targetError(command:MdmCommand,saved:JSONObject,state:MdmState):String {
  val p=command.probe?:return "invalid"
  val phased=command.kind==KIND
  if(command.kind !in listOf("vless_probe",KIND)||p.optString("mode")!=(if(phased)"phased" else ""))return "invalid"
  val action=p.optString("action");if(action !in listOf("start","retry","cancel"))return "invalid"
  if(action=="start")return ""
  val target=saved.keys().asSequence().map{saved.getJSONObject(it)}.firstOrNull{it.optString("runId")==p.optString("runId")}
  if(!phased)return if(target!=null&&target.optInt("schema",1)!=1)"unsupported" else ""
  if(target==null||target.optInt("schema",1)!=2||target.optString("sourceId")!=p.optString("sourceId")||target.optString("fingerprint")!=p.optString("fingerprint")||!VlessProbePolicy.owns(target,state))return "configuration_changed"
  return ""
 }
}
