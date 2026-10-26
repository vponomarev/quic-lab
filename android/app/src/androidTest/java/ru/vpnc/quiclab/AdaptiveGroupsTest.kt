package ru.vpnc.quiclab
import org.junit.Assert.*
import org.junit.Test
import org.json.JSONObject
import org.json.JSONArray
class AdaptiveGroupsTest {
 @Test fun individuallyImportedSubscriptionProfilesShareLogicalIdentity(){
  val group="33333333-3333-4333-8333-333333333333"
  fun uri(host:String)="vless://11111111-1111-4111-8111-111111111111@$host:443?security=tls&type=tcp&sni=example.com"
  val parsed=VlessSubscriptions.parse((uri("a.example")+"\n"+uri("b.example")).toByteArray())
  val merged=VlessSubscriptions.merge(JSONObject().put("profiles",JSONArray()),group,parsed)
  val profiles=merged.getJSONArray("profiles")
  profiles.put(JSONObject().put("id","standalone").put("identity",JSONObject().put("vless_uri",uri("c.example"))))
  val subscriptions=JSONObject().put(group,JSONObject().put("selected",JSONArray(parsed.entries.map{it.key})))
  val logical=AdaptiveGroups.logicalConnections(merged,subscriptions)
  assertEquals("subscription.$group",logical[profiles.getJSONObject(0).getString("id")])
  assertEquals("subscription.$group",logical[profiles.getJSONObject(1).getString("id")])
  assertNull(logical["standalone"])
  val grouped=VlessSubscriptions.mergeGroup(JSONObject().put("profiles",JSONArray()),group,"Subscription",parsed)
  val groupedId=grouped.getJSONArray("profiles").getJSONObject(0).getString("id")
  assertEquals("subscription.$group",AdaptiveGroups.logicalConnections(grouped,subscriptions)[groupedId])
 }
 @Test fun recoveredConnectionIsRetriedBeforeRemainingLargeSubscription(){
  val policy=VlessGroupPolicy(5,listOf("a","b","a","a","a"))
  policy.failed(0);assertEquals(1,policy.failed(1))
  policy.deferRetry(1,2)
  assertEquals(2,policy.retry(3));policy.deferRetry(2,4)
  assertEquals(1,policy.retry(30003))
 }
 @Test fun catalogBeyondLegacy32ParticipatesInFailover(){
  val policy=VlessGroupPolicy(256)
  policy.select(254);policy.failed(0)
  assertEquals(255,policy.failed(1))
 }
 @Test fun reserveEnvelopeKeepsSubscriptionAsOneConnection(){
  val uri="vless://11111111-1111-4111-8111-111111111111@127.0.0.1:443?security=tls&type=tcp&sni=example.com"
  val source=JSONObject().put("vless_uris",JSONArray().put(uri))
  val target=JSONObject().put("endpoint","gateway.invalid:443").put("certificate","cert").put("key","key")
  val runtime=VlessRelayRuntime.project("subscription-a","exit",source,target).getJSONObject("_exit_runtime")
  val profile=runtime.getJSONObject("model").getJSONArray("profiles").getJSONObject(0).getString("id")
  assertEquals("subscription-a",runtime.getJSONObject("logical_connections").getString(profile))
 }
}
