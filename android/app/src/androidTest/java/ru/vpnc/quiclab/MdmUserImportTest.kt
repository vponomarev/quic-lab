package ru.vpnc.quiclab

import mobile.Mobile
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class MdmUserImportTest {
 private fun document()=JSONObject("""{"schema":1,"profiles":[{"id":"default","name":"Existing","settings":{"transport":"quic","endpoint":"vpn.example:443"}}],"currentProfileId":"default","enabledProfileIds":["default"],"multiple":false,"globalApps":[],"dns":{"mode":"system","profileId":"default"},"budget":{"limitBytes":0},"diagnostics":{"enabled":false,"detailed":false}}""")
 private fun bundle(keys:Boolean=false):String {
  val doc=document();doc.getJSONArray("profiles").getJSONObject(0).put("name","Imported")
  if(keys)doc.getJSONArray("profiles").getJSONObject(0).put("identity",JSONObject().put("vless_uri","vless://11111111-1111-4111-8111-111111111111@example.org:443?security=tls&type=tcp&sni=example.org"))
  return Mobile.exportConfiguration(doc.toString(),"""{"sections":["profiles","routing","network","diagnostics"]}""",keys)
 }
 @Test fun addAndReplaceOnlyPrepareDraftAndPreserveAdministrativePolicy(){
  val before=document().put("profileSelection",JSONObject("""{"enabled":true,"profileIds":["default"],"serverRevision":7}"""))
  val saved=before.toString();val id="11111111-1111-4111-8111-111111111111"
  for(mode in listOf("add","replace")){
   val out=MdmUserImport.prepare(before,bundle(),mode,mapOf("default" to id))
   val candidate=out.getJSONObject("candidate")
   assertEquals(saved,before.toString());assertEquals(before.getJSONObject("profileSelection").toString(),candidate.getJSONObject("profileSelection").toString())
   assertEquals(if(mode=="add")2 else 1,candidate.getJSONArray("profiles").length())
   assertEquals("Imported",candidate.getJSONArray("profiles").getJSONObject(if(mode=="add")1 else 0).getString("name"))
  }
 }
 @Test fun updateTargetsDraftAndRejectsInvalidMapping(){
  val before=document();val raw=bundle()
  val out=MdmUserImport.prepare(before,raw,"update",mapOf("default" to "default"))
  assertEquals("Imported",out.getJSONObject("candidate").getJSONArray("profiles").getJSONObject(0).getString("name"))
  assertEquals("Existing",before.getJSONArray("profiles").getJSONObject(0).getString("name"))
  assertTrue(runCatching{MdmUserImport.prepare(before,raw,"update",mapOf("default" to "missing"))}.isFailure)
  assertTrue(runCatching{MdmUserImport.prepare(before,raw.replace("\"payload\":","\"rights\":{},\"payload\":"),"update",mapOf("default" to "default"))}.isFailure)
 }
 @Test fun encryptedKeysRequirePasswordAndPlaintextKeysAreRejected(){
  val raw=bundle(true)
  assertTrue(runCatching{MdmUserImport.decode(raw.toByteArray(),"")}.isFailure)
  val cipher=Mobile.encryptConfiguration(raw.toByteArray(),"test-password")
  assertTrue(runCatching{MdmUserImport.decode(cipher,"wrong-password")}.isFailure)
  val decoded=MdmUserImport.decode(cipher,"test-password")
  val out=MdmUserImport.prepare(document(),decoded,"update",mapOf("default" to "default"))
  assertTrue(out.getJSONObject("candidate").getJSONArray("profiles").getJSONObject(0).has("identity"))
  assertTrue(runCatching{MdmUserImport.decode(ByteArray(2*1024*1024+1),"")}.isFailure)
 }
}
