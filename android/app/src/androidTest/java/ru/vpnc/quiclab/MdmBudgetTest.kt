package ru.vpnc.quiclab

import org.junit.Assert.*
import org.junit.Test

class MdmBudgetTest {
 @Test fun controlSurvivesVpnStopAndKeepsSameMeter() {
  val run=VpnBudgetRun()
  val vpn=run.start(100)
  val control=run.acquireControl(100)
  assertSame(vpn,control)
  run.stop()
  assertSame(control,run.current)
  assertSame(control,run.start(100))
  run.releaseControl()
  run.stop()
  assertNull(run.current)
 }
 @Test fun teardownDoesNotDetachControlAccounting() {
  val run=VpnBudgetRun()
  val control=run.acquireControl(100)
  run.release()
  assertSame(control,run.current)
  run.releaseControl()
  assertNull(run.current)
 }
}
