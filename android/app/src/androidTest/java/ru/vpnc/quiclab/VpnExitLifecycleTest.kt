package ru.vpnc.quiclab

import org.junit.Assert.*
import org.junit.Test

class VpnExitLifecycleTest {
 private class Session: AutoCloseable { var closes=0; override fun close(){closes++} }
 @Test fun independentRestart() {
  val created=mutableListOf<Session>()
  val c=VpnExitController(setOf("home","internet")) { _,_ -> Session().also{created.add(it)} }
  val h=c.start("home");val old=c.start("internet")
  assertTrue(c.apply("home",h,"active"))
  val fresh=c.start("internet")
  assertEquals(0,created[0].closes);assertEquals(1,created[1].closes)
  assertFalse(c.apply("internet",old,"active"))
  assertEquals("active",c.state("home"))
  assertTrue(c.apply("internet",fresh,"incompatible"))
  assertEquals("incompatible",c.state("internet"))
  assertFalse(c.apply("internet",fresh,"active"))
  c.stopAll()
  assertTrue(created.all{it.closes==1})
  assertFalse(c.current("home",h))
  assertNull(c.session("home"))
 }
 @Test fun stoppedCallbackCannotMutateReplacement() {
  val c=VpnExitController(setOf("home")) { _,_ -> Session() }
  val old=c.start("home");c.stop("home");val fresh=c.start("home")
  var effects=0
  assertFalse(c.withCurrent("home",old){effects++})
  assertTrue(c.withCurrent("home",fresh){effects++})
  assertEquals(1,effects);c.stopAll()
 }
 @Test fun failedStartLeavesExitBlockedAndOtherExitAlive() {
  val home=Session()
  val c=VpnExitController(setOf("home","internet")) { id,_ -> if(id=="home")home else error("dial failed") }
  c.start("home")
  try {c.start("internet");fail("expected failure")}catch(_:IllegalStateException){}
  assertEquals("blocked",c.state("internet"));assertEquals(0,home.closes)
  c.stopAll();assertEquals(1,home.closes)
 }
 @Test fun stopAllClosesRemainingSessionsAfterOneCloseFails() {
  var closed=0
  val c=VpnExitController(setOf("home","internet")) { id,_ -> AutoCloseable {
   closed++; if(id=="home")throw IllegalStateException("close failed")
  } }
  c.start("home");c.start("internet")
  try {c.stopAll()}catch(_:IllegalStateException){}
  assertEquals(2,closed)
  assertNull(c.session("home"));assertNull(c.session("internet"))
 }
}
