package ru.vpnc.quiclab

import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test

class ProcessRecoveryTest {
 @Test fun everyProcessEntryHasManagementRecovery() {
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  assertEquals("Background jobs must initialize management recovery as well as diagnostics",
   "ru.vpnc.quiclab.QuicLabApplication",c.applicationInfo.className)
 }
 @Test fun interruptedVpnIsRecoveredFromBackgroundManagementEntry() {
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val ledger=java.io.File(c.cacheDir,"recovery-budget-"+System.nanoTime())
  try {
   val original=VpnBudgetRun(ledger,24)
   original.start(100);original.setResumeEligible(true);original.release()
   val restored=VpnBudgetRun(ledger,24)
   var starts=0
   VpnProcessRecovery.request(restored,false,true){starts++}
   assertEquals(1,starts)
   VpnProcessRecovery.request(restored,true,true){starts++}
   VpnProcessRecovery.request(restored,false,false){starts++}
   VpnProcessRecovery.request(VpnBudgetRun(ledger,25),false,true){starts++}
   assertEquals("Active VPN, missing consent and a new boot must not start",1,starts)
   restored.stop()
   VpnProcessRecovery.request(restored,false,true){starts++}
   assertEquals("Explicit Stop must win",1,starts)
  } finally { ledger.delete() }
 }
}
