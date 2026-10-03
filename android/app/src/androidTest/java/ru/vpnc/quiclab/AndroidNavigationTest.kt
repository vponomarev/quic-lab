package ru.vpnc.quiclab
import android.content.Intent
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry
import androidx.test.runner.lifecycle.Stage
import org.junit.Assert.*
import org.junit.Test
class AndroidNavigationTest {
 private fun views(v:View):List<View> = listOf(v)+(if(v is ViewGroup)(0 until v.childCount).flatMap{views(v.getChildAt(it))}else emptyList())
 private val inst get()=InstrumentationRegistry.getInstrumentation()
 private fun resumedEditor():ProfileEditorActivity {
  var found:ProfileEditorActivity?=null
  repeat(40){inst.runOnMainSync{found=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<ProfileEditorActivity>().firstOrNull()};if(found!=null)return found!!;Thread.sleep(100)}
  error("Editor did not open")
 }
 @Test fun editorStagesChangesAcrossRecreationAndSaveStaysOpen(){
  val c=inst.targetContext
  check(!LabVpnService.active)
  val selected=VpnProfiles.current(c).id
  val profile=VpnProfiles.create(c,"UI test temporary")
  try {
   VpnProfiles.preferences(c,profile.id).edit().putString("transport","quic").putString("endpoint","example.test:443").putString("hostname","example.test").putString("dns","1.1.1.1").putBoolean("global_apps",false).commit()
   VpnIdentity.writeBundle(c,profile.id,org.json.JSONObject().put("certificate","test only").put("subject","UI test"))
   VpnProfiles.select(c,selected)
   inst.uiAutomation.executeShellCommand("am start -W -a android.intent.action.MAIN -c android.intent.category.LAUNCHER -n ru.vpnc.quiclab/.MainActivity").use{android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()}
   Thread.sleep(600)
   inst.runOnMainSync{val root=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<VpnActivity>().first();root.startActivity(Intent(root,ProfileEditorActivity::class.java).putExtra("profile_id",profile.id))}
   var a=resumedEditor();Thread.sleep(400)
   inst.runOnMainSync{
    assertEquals(selected,VpnProfiles.current(c).id)
    views(a.window.decorView).filterIsInstance<android.widget.EditText>().first{it.text.toString()=="example.test:443"}.setText("example.test:444")
    assertEquals("example.test:443",VpnProfiles.preferences(c,profile.id).getString("endpoint",null))
    a.recreate()
   }
   Thread.sleep(700);a=resumedEditor();Thread.sleep(300)
   inst.runOnMainSync{
    assertTrue(views(a.window.decorView).filterIsInstance<android.widget.EditText>().any{it.text.toString()=="example.test:444"})
    views(a.window.decorView).filterIsInstance<Button>().first{it.text.toString()=="Сохранить"}.performClick()
    assertFalse(a.isFinishing)
    assertEquals("example.test:444",VpnProfiles.preferences(c,profile.id).getString("endpoint",null))
    assertEquals(selected,VpnProfiles.current(c).id)
    a.finish()
   }
  }finally{inst.runOnMainSync{ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<ProfileEditorActivity>().forEach{it.finish()}};VpnProfiles.select(c,selected);VpnProfiles.delete(c,profile.id)}
 }
 @Test fun rootSectionsDoNotRecreateActivity() {
  val inst=InstrumentationRegistry.getInstrumentation()
  inst.uiAutomation.executeShellCommand("am start -W -a android.intent.action.MAIN -c android.intent.category.LAUNCHER -n ru.vpnc.quiclab/.MainActivity").use { android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes() };Thread.sleep(1200)
  inst.runOnMainSync {
   val a=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<VpnActivity>().first()
   for(title in listOf("Подключения","Диагностика","Настройки","VPN")) {
    val b=views(a.window.decorView).filterIsInstance<Button>().firstOrNull{it.text.toString()==title}
    assertNotNull("Root navigation must contain $title",b);b!!.performClick();assertFalse(a.isFinishing);assertSame(a,ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<VpnActivity>().first())
   }
   assertFalse(views(a.window.decorView).any{it.contentDescription=="Назад"})
  }
 }
}
