package ru.vpnc.quiclab

import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class ResearchOwnerLookupTest {
 @Test fun dictionaryDistinguishesKnownSharedAndUnknown(){
  val known=JSONObject(ResearchOwnerLookup.identity(123,listOf("app.one"),listOf("One")))
  assertEquals("124",known.getString("id"))
  assertEquals("123",known.getString("uid"))
  assertEquals("known",known.getString("attribution"))
  val shared=JSONObject(ResearchOwnerLookup.identity(123,listOf("app.one","app.two"),listOf("One","Two")))
  assertEquals("shared_uid",shared.getString("attribution"))
  assertEquals(2,shared.getJSONArray("packages").length())
  assertEquals("unknown",JSONObject(ResearchOwnerLookup.identity(123,emptyList(),emptyList())).getString("attribution"))
 }
 @Test fun dictionaryIsBounded(){
  val bounded=JSONObject(ResearchOwnerLookup.identity(123,List(40){"p".repeat(2048)},List(40){"Я".repeat(2048)}))
  for(key in listOf("packages","labels")){
   val names=bounded.getJSONArray(key);assertEquals(32,names.length())
   for(i in 0 until names.length())assertTrue(names.getString(i).toByteArray(Charsets.UTF_8).size<=1024)
  }
 }
 @Test fun invalidLookupReturnsUnknownWithoutVpnPermissionOrDns(){
  val lookup=ResearchOwnerLookup(InstrumentationRegistry.getInstrumentation().targetContext)
  for(json in listOf(lookup.lookup(1,"127.0.0.1",123,"127.0.0.1",443),
   lookup.lookup(6,"not-a-numeric-address.invalid",123,"127.0.0.1",443),
   lookup.lookup(17,"127.0.0.1",-1,"127.0.0.1",443))){
   val value=JSONObject(json)
   assertEquals("unknown",value.getString("attribution"));assertEquals("-1",value.getString("uid"))
  }
 }
}
