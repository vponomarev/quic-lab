package ru.vpnc.quiclab
import org.json.JSONObject
internal object VlessProbePolicy {
 fun phasePlan(startKind:String):List<String> = when(startKind){"wifi"->listOf("wifi","cell");"cell"->listOf("cell");else->emptyList()}
 fun phaseBlockReason(kind:String,startKind:String,reserveAllowed:Boolean,budgetBlocked:Boolean):String = when {
  kind!="cell"->""
  budgetBlocked->"budget"
  startKind!="cell" && !reserveAllowed->"permission_denied"
  else->""
 }
 fun pinnedNetworkChanged(available:Boolean)=!available
 fun reportableV1(run:JSONObject)=run.optInt("schema",1)==1
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
