package ru.vpnc.quiclab

import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.json.JSONArray
import org.junit.Test
import org.junit.Assert.*
import java.io.File

/** Explicit maintenance action: update the saved subscription through the UI's transaction API. */
class FieldSubscriptionUpdateTest {
 private fun canonical(v:Any?):String=when(v){
  is JSONObject->v.keys().asSequence().toList().sorted().joinToString(prefix="{",postfix="}"){JSONObject.quote(it)+":"+canonical(v.get(it))}
  is JSONArray->(0 until v.length()).joinToString(prefix="[",postfix="]"){canonical(v.get(it))}
  is String->JSONObject.quote(v)
  else->v?.toString() ?: "null"
 }
 private fun protectedRuntime(raw:JSONObject,id:String):String {
  val r=JSONObject(raw.toString());r.remove("subscriptions")
  r.getJSONObject("identities").remove(id)
  r.getJSONObject("preferences").getJSONObject(if(id=="default")"vpn" else "vpn_$id").apply{remove("endpoint");remove("vless_endpoint")}
  return canonical(r)
 }
 @Test fun updateSavedSubscriptionPreservingSettings(){
  org.junit.Assume.assumeTrue(InstrumentationRegistry.getArguments().getString("update_subscription")=="true")
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  assertFalse("VPN must be inactive",LabVpnService.active)
  val subs=VlessSubscriptions.list(c);check(subs.length()==1)
  val group=subs.keys().next();val meta=subs.getJSONObject(group)
  check(meta.getBoolean("asGroup"))
  val id=java.util.UUID.nameUUIDFromBytes(("vless-group:"+group).toByteArray()).toString()
  val before=MdmConfiguration.snapshotRuntime(c)
  val original=File(c.noBackupFilesDir,"mdm-configuration.bin").readBytes()
  val file=File(c.filesDir,"subscription-update-report.json")
  val report=JSONObject().put("ok",false)
  file.writeText(report.toString())
  try {
   val parsed=VlessSubscriptions.fetch(meta.getString("url"))
   val preview=VlessSubscriptions.prepare(c,meta.getString("name"),meta.getString("url"),parsed,true)
   val candidate=JSONObject(preview.candidate)
   val expectedIdentity=(0 until candidate.getJSONArray("profiles").length()).map{candidate.getJSONArray("profiles").getJSONObject(it)}.single{it.getString("id")==id}.getJSONObject("identity")
   LocalConfigurationApply.apply(c,preview)
   val after=MdmConfiguration.snapshotRuntime(c)
   assertTrue("Other settings changed",protectedRuntime(before,id)==protectedRuntime(after,id))
   val actual=VpnIdentity.load(c,id).getJSONArray("vless_uris")
   assertTrue("Saved subscription differs from preview",canonical(expectedIdentity.getJSONArray("vless_uris"))==canonical(actual))
   val saved=(0 until actual.length()).map{mobile.Mobile.importVLESSConfig(actual.getString(it))}.toSet()
   val fresh=parsed.entries.map{mobile.Mobile.importVLESSConfig(it.uri)}.toSet()
   assertTrue("Fresh nodes missing",saved.containsAll(fresh))
   val updated=VlessSubscriptions.list(c).getJSONObject(group)
   for(key in listOf("name","url","asGroup"))assertTrue("Subscription metadata changed",meta.get(key)==updated.get(key))
   assertTrue(updated.getLong("updatedAt")>=meta.getLong("updatedAt"))
   assertFalse(LabVpnService.active)
   report.put("ok",true).put("fresh_nodes",fresh.size).put("stored_nodes",saved.size).put("retained_absent_nodes",(saved-fresh).size)
    .put("other_settings_unchanged",true).put("vpn_active",false).put("updated_at",updated.getLong("updatedAt"))
  }catch(t:Throwable){
   val atomic=android.util.AtomicFile(File(c.noBackupFilesDir,"mdm-configuration.bin"));val out=atomic.startWrite()
   try{out.write(original);atomic.finishWrite(out)}catch(e:Throwable){atomic.failWrite(out);throw e}
   report.put("rolled_back",true).put("error_class",t.javaClass.simpleName)
   throw t
  }finally{file.writeText(report.toString())}
 }
}
