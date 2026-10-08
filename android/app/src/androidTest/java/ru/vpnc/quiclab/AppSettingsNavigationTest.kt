package ru.vpnc.quiclab

import android.content.Intent
import android.view.View
import android.view.ViewGroup
import android.widget.*
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry
import androidx.test.runner.lifecycle.Stage
import org.junit.Assert.*
import org.junit.Test

class AppSettingsNavigationTest {
 private val inst get()=InstrumentationRegistry.getInstrumentation()
 private fun views(v:View):List<View> = listOf(v)+(if(v is ViewGroup)(0 until v.childCount).flatMap{views(v.getChildAt(it))}else emptyList())
 private fun editor():AppSettingsActivity {
  var a:AppSettingsActivity?=null
  repeat(50){inst.runOnMainSync{a=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<AppSettingsActivity>().firstOrNull()};if(a!=null)return a!!;Thread.sleep(100)}
  error("Settings did not open")
 }
 private fun dirty(a:AppSettingsActivity)=AppSettingsActivity::class.java.getDeclaredMethod("dirty").apply{isAccessible=true}.invoke(a) as Boolean
 private fun scenario(check:(AppSettingsActivity)->Unit) {
  val c=inst.targetContext
  val sources=listOf(VpnRttSettings.preferences(c),VpnReserveSettings.preferences(c),VpnProfiles.meta(c))
  val backups=sources.map{it.all.toMap()}
  try {
   sources[0].edit().remove("screen_on").remove("screen_off").commit()
   sources[1].edit().remove("cell_on").commit()
   sources[2].edit().remove("dns_mode").commit()
   val before=sources.map{it.all.toMap()}
   inst.uiAutomation.executeShellCommand("am start -W -a android.intent.action.MAIN -c android.intent.category.LAUNCHER -n ru.vpnc.quiclab/.MainActivity").use{android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()}
   Thread.sleep(500)
   inst.runOnMainSync{val root=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).first();root.startActivity(Intent(root,AppSettingsActivity::class.java))}
   val a=editor();Thread.sleep(800)
   check(a)
   assertEquals("Viewing or abandoning settings must not persist defaults",before,sources.map{it.all.toMap()})
  } finally {
   inst.runOnMainSync{ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<AppSettingsActivity>().forEach{it.finish()}}
   sources.zip(backups).forEach{(source,backup)->PreferenceDraft(source).apply{restore(backup);persist()}}
  }
 }
 @Test fun viewingSettingsLeavesWithoutSavePrompt()=scenario {a->
  inst.runOnMainSync{assertFalse("Untouched settings are dirty",dirty(a));a.onBackPressed();assertTrue(a.isFinishing)}
 }
 @Test fun returningControlsToOriginalValuesIsNotAnEdit()=scenario {a->
  inst.runOnMainSync{views(a.window.decorView).filterIsInstance<Spinner>().first().setSelection(1)}
  Thread.sleep(300)
  inst.runOnMainSync{assertTrue(dirty(a));views(a.window.decorView).filterIsInstance<Spinner>().first().setSelection(0)}
  Thread.sleep(300)
  inst.runOnMainSync{
   val toggle=views(a.window.decorView).filterIsInstance<Switch>().first{it.text.toString().contains("Готовить резерв LTE")}
   toggle.isChecked=true;assertTrue(dirty(a));toggle.isChecked=false
   assertFalse("Reverted controls are dirty",dirty(a))
  }
 }
 @Test fun realEditSurvivesRecreation()=scenario {a->
  inst.runOnMainSync{views(a.window.decorView).filterIsInstance<EditText>().first().setText("123");assertTrue(dirty(a));a.recreate()}
  Thread.sleep(800)
  val recreated=editor()
  inst.runOnMainSync{assertTrue("Recreation lost pending edit",dirty(recreated));assertEquals("123",views(recreated.window.decorView).filterIsInstance<EditText>().first().text.toString())}
 }
}
