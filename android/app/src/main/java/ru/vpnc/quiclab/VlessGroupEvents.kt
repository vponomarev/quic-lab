package ru.vpnc.quiclab
import org.json.JSONArray
import org.json.JSONObject
internal object VlessGroupEvents {
 private val events=ArrayDeque<JSONObject>()
 @Synchronized fun record(e:JSONObject){
  if(e.optString("event")!="vless_group")return
  val last=events.lastOrNull()
  if(last?.optString("reason")==e.optString("reason") && last.optInt("index")==e.optInt("index"))return
  events.addLast(JSONObject().put("index",e.getInt("index")).put("count",e.getInt("count")).put("reason",e.getString("reason")).put("time",e.getLong("time")))
  while(events.size>32)events.removeFirst()
 }
 @Synchronized fun snapshot()=JSONArray(events.map{JSONObject(it.toString())})
}
