package ru.vpnc.quiclab
import org.json.JSONArray
import org.json.JSONObject

/** Bounded status only; never retain the credential-bearing runtime envelope. */
internal object VlessRelayStatus {
 @Volatile private var value:JSONObject?=null
 private var lostSessions=0
 private var lastLost=""
 @Synchronized fun update(runtime:JSONObject,snapshot:JSONObject,lost:Boolean=false){
  val connection=snapshot.optString("connection_id")
  if(lost && connection!=lastLost){lostSessions=minOf(lostSessions+1,1000000);lastLost=connection}
  val model=runtime.getJSONObject("model");val profiles=model.getJSONArray("profiles")
  val indices=(0 until profiles.length()).associate{profiles.getJSONObject(it).getString("id") to it+1}
  val paths=JSONArray();val source=snapshot.optJSONArray("paths")?:JSONArray()
  for(i in 0 until minOf(source.length(),2)){
   val p=source.getJSONObject(i);val index=indices[p.optString("profile_id")]?:continue
   paths.put(JSONObject().put("index",index).put("network",p.getString("network")).put("ready",p.optBoolean("ready"))
    .put("primary",p.optString("path_id")==snapshot.optString("preferred")))
  }
  val ready=(0 until paths.length()).count{paths.getJSONObject(it).optBoolean("ready")}
  val state=when{lost->"session_lost";!snapshot.optBoolean("running")->"stopped";ready==2->"ready";ready==1->"degraded";else->"no_path"}
  value=JSONObject().put("exitId",model.getJSONArray("exits").getJSONObject(0).getString("id"))
   .put("generation",connection.removePrefix("bond-").toLongOrNull()?:snapshot.optLong("generation")).put("lostSessions",lostSessions).put("state",state).put("paths",paths)
 }
 @Synchronized fun clear(){value=null;lostSessions=0;lastLost=""}
 fun snapshot():JSONObject?=value?.let{JSONObject(it.toString())}
}
