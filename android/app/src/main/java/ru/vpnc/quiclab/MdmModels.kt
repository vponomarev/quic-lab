package ru.vpnc.quiclab

import org.json.JSONArray
import org.json.JSONObject
import java.net.URI
import java.time.Instant

internal data class MdmRights(val config:Boolean=false,val vpn:Boolean=false,val telemetry:Boolean=false,
 val geo:Boolean=false,val coordinates:Boolean=false,val lanMode:String="deny") {
 init{require(lanMode in listOf("deny","confirm","allow"))}
 fun json()=JSONObject().put("config",config).put("vpn",vpn).put("telemetry",telemetry)
  .put("geo",geo).put("coordinates",coordinates).put("lanMode",lanMode)
 companion object {
  fun parse(j:JSONObject)=MdmRights(j.optBoolean("config"),j.optBoolean("vpn"),j.optBoolean("telemetry"),
   j.optBoolean("geo"),j.optBoolean("coordinates"),j.optString("lanMode","deny").ifEmpty{"deny"})
 }
}
/** No credentials in this display/wire identity. Secret storage belongs to MdmStore. */
internal data class MdmBinding(val id:String,val endpoint:String,val epoch:Long,val requestedRights:MdmRights,val active:Boolean) {
 init {
  val u=URI(endpoint)
  require(u.scheme=="https" && u.host!=null && u.rawUserInfo==null && u.rawQuery==null && u.fragment==null &&
   (u.path.isEmpty() || u.path=="/") && (u.port==-1 || u.port in 1..65535)) { "Неверный адрес MDM" }
 }
 fun url(operation:String):String {
  require(operation in listOf("enroll","activate","sync","pause"))
  return endpoint.trimEnd('/')+"/mdm/v1/"+operation
 }
}
internal data class MdmSyncRequest(val bindingId:String,val epoch:Long,val appliedRevision:Long=0,
 val events:JSONArray=JSONArray(),val wait:Boolean=true) {
 fun json()=JSONObject().put("version",1).put("bindingId",bindingId).put("epoch",epoch)
  .put("appliedRevision",appliedRevision).put("events",events).put("wait",wait)
}
internal data class MdmConfigRevision(val revision:Long,val mode:String,val document:JSONObject)
internal data class MdmCommand(val id:String,val bindingId:String,val epoch:Long,val issuedAt:Instant,
 val expiresAt:Instant,val kind:String)
internal data class MdmSyncResponse(val epoch:Long,val desiredConfig:MdmConfigRevision?,val commands:List<MdmCommand>,
 val serverTime:Instant) {
 companion object {
  fun parse(raw:String):MdmSyncResponse {
   val j=JSONObject(raw);require(j.getInt("version")==1)
   val config=j.optJSONObject("desiredConfig")?.let {
    val mode=it.getString("mode");require(mode=="current"||mode=="external")
    MdmConfigRevision(it.getLong("revision"),mode,it.getJSONObject("document"))
   }
   val commands=j.optJSONArray("commands")?:JSONArray();require(commands.length()<=100)
   return MdmSyncResponse(j.getLong("epoch"),config,(0 until commands.length()).map { i ->
    val c=commands.getJSONObject(i);val kind=c.getString("kind");require(kind=="vpn_start"||kind=="vpn_stop")
    MdmCommand(c.getString("id"),c.getString("bindingId"),c.getLong("epoch"),
     Instant.parse(c.getString("issuedAt")),Instant.parse(c.getString("expiresAt")),kind)
   },Instant.parse(j.getString("serverTime")))
  }
 }
}
