package ru.vpnc.quiclab
import org.json.JSONObject
internal object MdmProfileSelection {
 fun allows(runtime:JSONObject?,id:String):Boolean {
  if(runtime==null)return false
  val meta=runtime.optJSONObject("preferences")?.optJSONObject("vpn_profiles")?:return false
  val policy=runtime.optJSONObject("profileSelection")?:return false
  val ids=policy.optJSONArray("profileIds")?:return false
  return !meta.optBoolean("multiple") && policy.optBoolean("enabled") && (0 until ids.length()).any{ids.optString(it)==id}
 }
 fun carry(doc:JSONObject,previous:JSONObject,next:JSONObject) {
  val policy=doc.optJSONObject("profileSelection")
  if(policy!=null)next.put("profileSelection",JSONObject(policy.toString()))
  val server=doc.getString("currentProfileId")
  next.put("serverSelectedProfile",server)
  val old=previous.optJSONObject("profileSelection")
  val selected=previous.optJSONObject("preferences")?.optJSONObject("vpn_profiles")?.optString("current").orEmpty()
  if(old?.optBoolean("enabled")==true && policy!=null && previous.optString("serverSelectedProfile")==server && old.optLong("serverRevision")==policy.optLong("serverRevision") && allows(next,selected)){
   next.getJSONObject("preferences").getJSONObject("vpn_profiles").put("current",selected)
  }
 }
}
