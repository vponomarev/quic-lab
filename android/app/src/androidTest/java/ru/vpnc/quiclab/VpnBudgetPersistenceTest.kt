package ru.vpnc.quiclab
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File
class VpnBudgetPersistenceTest {
 @Test fun processReplacementKeepsEpochUsageAndExhaustion(){
  val f=File(InstrumentationRegistry.getInstrumentation().targetContext.cacheDir,"budget-test-"+System.nanoTime())
  try{
   f.writeText("""{"epoch":"previous","limit":100,"used":90,"reserved":0,"blocked":true,"by_class":{"user":90}}""")
   val run=VpnBudgetRun(f);val meter=run.start(100)
   assertEquals(90,JSONObject(meter.snapshot()).getInt("used"));assertFalse(meter.cellAllowed())
   run.checkpoint()
   val next=VpnBudgetRun(f).start(100)
   assertEquals("previous",JSONObject(next.snapshot()).getString("epoch"))
   run.stop();assertFalse(f.exists())
   assertNotEquals("previous",JSONObject(VpnBudgetRun(f).start(100).snapshot()).getString("epoch"))
  }finally{f.delete()}
 }
 @Test fun explicitStopAndRebootDoNotResume(){
  val f=File(InstrumentationRegistry.getInstrumentation().targetContext.cacheDir,"budget-boot-"+System.nanoTime())
  try{
   val run=VpnBudgetRun(f,4);run.start(100);assertFalse(run.shouldResume());run.setResumeEligible(true)
   assertTrue(VpnBudgetRun(f,4).shouldResume());assertFalse(VpnBudgetRun(f,5).shouldResume())
   run.setResumeEligible(false);run.release();assertFalse(VpnBudgetRun(f,4).shouldResume())
   assertEquals(100,JSONObject(VpnBudgetRun(f,4).start(100).snapshot()).getInt("limit"))
   run.stop();assertFalse(VpnBudgetRun(f,4).shouldResume())
  }finally{f.delete()}
 }
 @Test fun changedLimitPreservesUsageAndEpoch(){
  val f=File(InstrumentationRegistry.getInstrumentation().targetContext.cacheDir,"budget-limit-"+System.nanoTime())
  try{
   f.writeText("""{"epoch":"previous","limit":100,"used":90,"reserved":0,"blocked":true,"by_class":{"user":90}}""")
   val raised=VpnBudgetRun(f).start(200)
   assertTrue(raised.cellAllowed());assertEquals(90,JSONObject(raised.snapshot()).getInt("used"))
   val lowered=VpnBudgetRun(f).start(50)
   assertFalse(lowered.cellAllowed());assertEquals("previous",JSONObject(lowered.snapshot()).getString("epoch"))
  }finally{f.delete()}
 }
 @Test fun invalidAccountingCannotTriggerAutomaticResume(){
  val f=File(InstrumentationRegistry.getInstrumentation().targetContext.cacheDir,"budget-invalid-"+System.nanoTime())
  try{
   f.writeText("""{"epoch":"bad","used":-1,"limit":100,"boot_count":4,"resume":true}""")
   assertFalse(VpnBudgetRun(f,4).shouldResume())
  }finally{f.delete()}
 }
 @Test fun corruptCheckpointDoesNotSilentlyResetBudget(){
  val f=File(InstrumentationRegistry.getInstrumentation().targetContext.cacheDir,"budget-bad-"+System.nanoTime())
  try{f.writeText("{broken");assertTrue(runCatching{VpnBudgetRun(f).start(100)}.isFailure);VpnBudgetRun(f).stop();assertEquals(0,JSONObject(VpnBudgetRun(f).start(100).snapshot()).getInt("used"))}finally{f.delete()}
 }
}
