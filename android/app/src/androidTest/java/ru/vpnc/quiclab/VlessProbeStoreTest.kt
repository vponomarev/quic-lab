package ru.vpnc.quiclab
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Test
import org.junit.Assert.*
import org.json.JSONObject
import org.json.JSONArray
class VlessProbeStoreTest{
 @Test fun aggregateLookupSurvivesReorderButInvalidatesChangedMembers(){
  val a=ProbeSource("a","A",listOf(ProbeNode("a1","A1","a.example:443",JSONObject().put("UUID","one"))))
  val b=ProbeSource("b","B",listOf(ProbeNode("b1","B1","b.example:443",JSONObject().put("UUID","two"))))
  val original=findProbeSource(listOf(a,b),ALL_PROBE_SOURCE_ID)?:error("aggregate missing")
  assertEquals(2,original.nodes.size)
  assertEquals(original.fingerprint,findProbeSource(listOf(b,a),ALL_PROBE_SOURCE_ID)!!.fingerprint)
  assertNotEquals(original.fingerprint,findProbeSource(listOf(a),ALL_PROBE_SOURCE_ID)!!.fingerprint)
  val changed=b.copy(nodes=listOf(b.nodes.single().copy(config=JSONObject().put("UUID","rotated"))))
  assertNotEquals(original.fingerprint,findProbeSource(listOf(a,changed),ALL_PROBE_SOURCE_ID)!!.fingerprint)
  assertSame(a,findProbeSource(listOf(a,b),"a"));assertNull(findProbeSource(listOf(a,b),"missing"))
  assertNull(findProbeSource(emptyList(),ALL_PROBE_SOURCE_ID))
 }
 @Test fun allSubscriptionsAreExpandedWithSourceLabels(){
  val a=ProbeSource("a","Subscription A",listOf(ProbeNode("same","node A","a.example:443",JSONObject().put("Endpoint","a.example:443"))))
  val b=ProbeSource("b","Subscription B",listOf(ProbeNode("same","node B","b.example:443",JSONObject().put("Endpoint","b.example:443"))))
  val combined=allProbeSource(listOf(a,b))?:error("aggregate missing")
  assertEquals(2,combined.nodes.size);assertEquals(ALL_PROBE_SOURCE_ID,combined.id)
  assertEquals(setOf("Subscription A","Subscription B"),combined.nodes.map{it.sourceName}.toSet())
 assertNotEquals(combined.nodes[0].id,combined.nodes[1].id)
 }

 @Test fun changedCredentialsInvalidateProbeFingerprint(){
  val before=ProbeSource("subscription","Subscription",listOf(ProbeNode("node","Node","node.example:443",JSONObject().put("Endpoint","node.example:443").put("UUID","old-credential"))))
  val after=ProbeSource("subscription","Subscription",listOf(ProbeNode("node","Node","node.example:443",JSONObject().put("Endpoint","node.example:443").put("UUID","new-credential"))))
  assertNotEquals(before.fingerprint,after.fingerprint)
 }

 @Test fun stagesShowIndependentExitIP(){
 val row=JSONObject().put("dnsOutcome","success").put("tcpOutcome","success").put("proxyOutcome","confirmed").put("httpsOutcome","timeout").put("httpsStage","http_response").put("exitIpOutcome","success").put("exitIp","203.0.113.8")
 val text=probeStageSummary(row)
 assertTrue(text.contains("203.0.113.8"));assertTrue(text.contains("ожидание HTTP"));assertTrue(text.contains("подтверждено HTTPS"))
 assertTrue(probeStageSummary(JSONObject()).contains("повторите проверку"))
 }

 @Test fun proxySniProjectionDoesNotLeakCredentials(){
  val node=ProbeNode("node","Node","proxy.example:443",JSONObject().put("ServerName","cover.example").put("UUID","secret").put("PublicKey","key"))
  val row=node.publicRow()
  assertEquals("cover.example",row.getString("serverName"))
  assertEquals("proxy.example:443",row.getString("endpoint"))
  assertFalse(row.toString().contains("secret"));assertFalse(row.has("PublicKey"))
  val legacy=ProbeNode("old","Old","proxy.example:443",JSONObject()).publicRow()
  assertEquals("",legacy.getString("serverName"))
 }

 @Test fun recoveryInterruptsEveryPendingPhase(){
 val real=InstrumentationRegistry.getInstrumentation().targetContext
 val dir=java.io.File(real.cacheDir,"probe-phase-store-"+System.nanoTime()).apply{mkdirs()}
 val c=object:android.content.ContextWrapper(real){override fun getFilesDir()=dir}
 try{
  val row=JSONObject().put("outcome","queued")
  val phases=JSONObject().put("wifi",JSONObject().put("state","finished").put("rows",JSONArray().put(JSONObject().put("outcome","success"))))
   .put("cell",JSONObject().put("state","queued").put("rows",JSONArray().put(row)))
  val s=VlessProbeStore(c);s.save(JSONObject().put("sourceId","s").put("schema",2).put("state","running").put("rows",JSONArray().put(row)).put("phases",phases))
  s.recover();val p=s.all().getJSONObject("s").getJSONObject("phases")
  assertEquals("success",p.getJSONObject("wifi").getJSONArray("rows").getJSONObject(0).getString("outcome"))
  assertEquals("interrupted",p.getJSONObject("cell").getString("state"))
  assertEquals("interrupted",p.getJSONObject("cell").getJSONArray("rows").getJSONObject(0).getString("outcome"))
 }finally{dir.deleteRecursively()}
 }
 @Test fun recoveryPreservesCompletedRowsAndBoundsSources(){
 val real=InstrumentationRegistry.getInstrumentation().targetContext
 val dir=java.io.File(real.cacheDir,"probe-store-test-"+System.nanoTime()).apply{mkdirs()}
 val c=object:android.content.ContextWrapper(real){override fun getFilesDir()=dir}
 try{val s=VlessProbeStore(c);for(i in 0..32)s.save(JSONObject().put("sourceId","s$i").put("runId","r$i").put("startedAt","2026-10-17T00:00:%02dZ".format(i)).put("state","running").put("sequence",1).put("rows",JSONArray().put(JSONObject().put("outcome","success")).put(JSONObject().put("outcome","running"))))
 assertEquals(32,s.all().length());assertFalse(s.all().has("s0"));s.recover();val r=s.all().getJSONObject("s32");assertEquals("interrupted",r.getString("state"));assertEquals("success",r.getJSONArray("rows").getJSONObject(0).getString("outcome"));assertEquals("interrupted",r.getJSONArray("rows").getJSONObject(1).getString("outcome"))
 }finally{dir.deleteRecursively()}
 }
}
