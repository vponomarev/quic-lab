package ru.vpnc.quiclab

import org.junit.Assert.*
import org.junit.Test

class MdmQuietPassGateTest {
 @Test fun saveAfterPollOwesAnotherPassWhenImmediateJobCollides(){
  val gate=MdmQuietPassGate()
  assertTrue(gate.start()) // periodic pass has polled, still reporting/uploading
  assertFalse(gate.start()) // save schedules immediate job while periodic is active
  assertFalse(gate.start()) // repeated kicks coalesce
  assertTrue(gate.finish()) // completion must schedule a fresh poll
  assertTrue(gate.start())
  assertFalse(gate.finish())
 }
 @Test fun cancellationDropsOwedWorkButKeepsRunningLeaseUntilDrained(){
  val gate=MdmQuietPassGate()
  assertTrue(gate.start());assertFalse(gate.start())
  gate.cancel()
  assertFalse(gate.finish())
  assertTrue(gate.start());assertFalse(gate.finish())
 }
}
