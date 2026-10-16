package ru.vpnc.quiclab
import org.junit.Assert.*
import org.junit.Test
class VlessGroupPolicyTest {
 @Test fun twoFailuresCooldownAndRecovery(){
  val p=VlessGroupPolicy(2)
  assertNull(p.failed(0));assertEquals(1,p.failed(1));assertEquals(0,p.current);p.select(1)
  assertNull(p.failed(2));assertNull(p.failed(3));assertNull(p.retry(29999))
  assertEquals(0,p.retry(30001));p.select(0);p.healthy();assertNull(p.failed(30002))
 }
 @Test fun failedCandidateDoesNotChangeActive(){
  val p=VlessGroupPolicy(3);p.failed(0);assertEquals(1,p.failed(1));p.deferRetry(1,2)
  assertEquals(0,p.current);assertEquals(2,p.retry(3));p.select(2);assertEquals(2,p.current)
 }
 @Test fun longRoundExhaustionSurvivesExpiredCooldowns(){
  val p=VlessGroupPolicy(4)
  for(i in 0..3){p.select(i);p.failed(i*40000L);p.failed(i*40000L+1)}
  assertTrue(p.exhausted(120002))
  p.resetNetwork();assertFalse(p.exhausted(120003))
 }
 @Test fun singleCandidateAndBounds(){
  val p=VlessGroupPolicy(1);assertNull(p.failed(0));assertNull(p.failed(1));assertEquals(0,p.retry(30001))
  try{VlessGroupPolicy(33);fail("unbounded")}catch(_:IllegalArgumentException){}
 }
}
