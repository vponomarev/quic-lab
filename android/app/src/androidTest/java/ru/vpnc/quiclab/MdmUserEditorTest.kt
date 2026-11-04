package ru.vpnc.quiclab

import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class MdmUserEditorTest {
 private val binding=MdmBinding("own-device","https://mdm.example",7,MdmRights(config=true),true)
 private fun state()=MdmState(binding,true,9,MdmRights(config=true))
 @Test fun requestUsesOwnBindingAndDoesNotApplyLocally(){
  var current=state();var kick=0;var sent:JSONObject?=null
  val session=MdmUserEditorSession({current},{"private-secret"},{b,op,body,secret->
   assertEquals(binding,b);assertEquals("config-submit",op);assertEquals("private-secret",secret);sent=body
   "{\"revision\":8}"
  },{kick++})
  val draft=JSONObject().put("policyRevision",2).put("expectedRevision",7).put("expectedGeneration",10).put("requestId","save-1").put("document",JSONObject().put("schema",1))
  assertEquals(8,session.request("save",draft.toString()).getLong("revision"))
  assertEquals("own-device",sent!!.getString("bindingId"));assertEquals(7,sent!!.getLong("epoch"));assertEquals(1,kick)
  assertEquals(0,current.appliedRevision)
  draft.put("bindingId","other")
  assertTrue(runCatching{session.request("save",draft.toString())}.isFailure)
  assertTrue(runCatching{session.request("admin", "{}")} .isFailure)
 }
 @Test fun pauseDuringRequestRejectsLateResponse(){
  var current=state();var kick=0
  val session=MdmUserEditorSession({current},{"secret"},{_,_,_,_->current=current.copy(active=false,localGeneration=10);"{\"revision\":8}"},{kick++})
  assertTrue(runCatching{session.request("read","{}")}.isFailure)
  assertEquals(0,kick)
 }
 @Test fun generationChangeAndCloseFenceEditor(){
  var current=state();var calls=0
  val session=MdmUserEditorSession({current},{"secret"},{_,_,_,_->calls++;"{}"},{})
  current=current.copy(localGeneration=10)
  assertTrue(runCatching{session.request("read","{}")}.isFailure);assertEquals(0,calls)
  session.close();current=state()
  assertTrue(runCatching{session.request("read","{}")}.isFailure);assertEquals(0,calls)
 }
 @Test fun assetsAndURLsAreConfined(){
  assertEquals("mdm/mdm_phone.html",MdmUserEditorAssets.path("https://mdm-editor.invalid/index.html"))
  assertEquals("mdm/mdm_editor.js",MdmUserEditorAssets.path("https://mdm-editor.invalid/mdm_editor.js"))
  for(url in listOf("http://mdm-editor.invalid/index.html","https://evil.example/index.html","file:///sdcard/test","https://mdm-editor.invalid/../index.html","https://mdm-editor.invalid/index.html?x=1","https://mdm-editor.invalid:8443/index.html"))assertNull(url,MdmUserEditorAssets.path(url))
  assertEquals("https://mdm.example/mdm/v1/config-submit",binding.url("config-submit"))
 }
}
