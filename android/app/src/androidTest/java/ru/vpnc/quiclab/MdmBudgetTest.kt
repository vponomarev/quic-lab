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

 @Test fun controlStartupPreservesVpnResumeWithinBootButNotAcrossBoots(){
  val c=androidx.test.platform.app.InstrumentationRegistry.getInstrumentation().targetContext
  val file=java.io.File(c.filesDir,"mdm-resume-test-"+System.nanoTime())
  try{
   val before=VpnBudgetRun(file,7);before.start(100);before.setResumeEligible(true);before.release()
   assertTrue(VpnBudgetRun(file,7).shouldResume())
   val control=VpnBudgetRun(file,7);control.acquireControl(100)
   assertTrue("MDM erased the user's VPN recovery intent",VpnBudgetRun(file,7).shouldResume())
   control.releaseControl()
   val reboot=VpnBudgetRun(file,8);reboot.acquireControl(100)
   assertFalse("MDM enabled VPN autostart across reboot",VpnBudgetRun(file,8).shouldResume())
   reboot.releaseControl()
  }finally{file.delete()}
 }
}
