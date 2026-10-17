package ru.vpnc.quiclab
import org.json.JSONObject
internal object VlessProbePolicy {
 fun owns(run:JSONObject,state:MdmState):Boolean {
  if(run.optString("initiator")!="mdm")return true
  return state.active && !state.cleanupPending && state.rights.vpn && state.rights.telemetry &&
   run.optString("ownerBinding")==state.binding?.id && run.optLong("ownerEpoch",-1)==state.binding?.epoch &&
   run.optLong("ownerGeneration",-1)==state.localGeneration
 }
 fun normalize(result:JSONObject,cell:Boolean,blocked:Boolean):JSONObject {
  if(cell && blocked && result.optString("outcome") in listOf("transport","timeout","relay_failed"))result.put("outcome","budget")
  return result
 }
 fun networkChanged(selected:Long,current:Long?,available:Boolean)=!available || current!=selected
}
