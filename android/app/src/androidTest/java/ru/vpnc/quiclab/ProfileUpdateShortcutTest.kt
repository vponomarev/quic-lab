package ru.vpnc.quiclab
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.os.ParcelFileDescriptor
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry
import androidx.test.runner.lifecycle.Stage
import org.junit.Assert.*
import org.junit.Test
class ProfileUpdateShortcutTest {
 @Test fun shortcutTargetsExistingProfileWithoutRegistration(){
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext
  val p=VpnProfiles.current(c);val prefs=VpnProfiles.preferences(c,p.id)
  val had=prefs.contains("managed_profile");val managed=prefs.getBoolean("managed_profile",false)
  val ids=VpnProfiles.list(c).map{it.id}
  val before=VpnProfiles.identityFile(c,p.id).readBytes()
  fun views(v:View):List<View> = listOf(v)+if(v is ViewGroup)(0 until v.childCount).flatMap{views(v.getChildAt(it))}else emptyList()
  try{
   prefs.edit().putBoolean("managed_profile",true).commit()
   inst.uiAutomation.executeShellCommand("am start -W -a android.intent.action.MAIN -c android.intent.category.LAUNCHER -n ru.vpnc.quiclab/.MainActivity").use{ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()}
   Thread.sleep(700)
   inst.runOnMainSync{
    val a=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<VpnActivity>().first()
    views(a.window.decorView).filterIsInstance<Button>().first{it.text=="Подключения"}.performClick()
    views(a.window.decorView).filterIsInstance<Button>().first{it.text=="Обновить настройки"}.performClick()
   }
   Thread.sleep(1000)
   inst.runOnMainSync{
    val a=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<ProfileEditorActivity>().first()
    assertEquals(p.id,a.intent.getStringExtra("profile_id"));assertFalse(a.intent.hasExtra("update_profile"));a.finish()
   }
   assertEquals(ids,VpnProfiles.list(c).map{it.id})
   assertArrayEquals(before,VpnProfiles.identityFile(c,p.id).readBytes())
  }finally{prefs.edit().apply{if(had)putBoolean("managed_profile",managed)else remove("managed_profile")}.commit()}
 }
}
