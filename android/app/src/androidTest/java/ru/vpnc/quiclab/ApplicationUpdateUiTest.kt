package ru.vpnc.quiclab
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.TextView
import android.os.ParcelFileDescriptor
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry
import androidx.test.runner.lifecycle.Stage
import org.junit.Assert.*
import org.junit.Test
import org.json.JSONArray
import org.json.JSONObject
class ApplicationUpdateUiTest {
 @Test fun updateButtonOpensSettingsAndDisappearsForCurrentVersion(){
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext
  val prefs=c.getSharedPreferences("application_updates",0)
  val saved=prefs.all
  fun offer(code:Int)=JSONArray().put(JSONObject().put("profile",VpnProfiles.current(c).id).put("server","test.example").put("android_version_code",code).put("android_version_name","test-version").put("apk_url","https://test.example/client.apk")).toString()
  fun views(v:View):List<View> = listOf(v)+if(v is ViewGroup)(0 until v.childCount).flatMap{views(v.getChildAt(it))}else emptyList()
  try {
   prefs.edit().putString("offers",offer(BuildConfig.VERSION_CODE+1)).putLong("checked",System.currentTimeMillis()).commit()
   inst.uiAutomation.executeShellCommand("am start -W -a android.intent.action.MAIN -c android.intent.category.LAUNCHER -n ru.vpnc.quiclab/.MainActivity").use{ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()}
   var activity:VpnActivity?=null
   repeat(100){if(activity==null){inst.runOnMainSync{activity=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<VpnActivity>().firstOrNull()};Thread.sleep(100)}}
   assertNotNull(activity)
   inst.runOnMainSync{
    val a=activity!!
    views(a.window.decorView).filterIsInstance<Button>().first{it.text=="Обновить"&&it.visibility==View.VISIBLE}.performClick()
    assertTrue(views(a.window.decorView).filterIsInstance<TextView>().any{it.text.contains("На сервере test.example")})
    assertTrue(views(a.window.decorView).filterIsInstance<Button>().count{it.text=="Обновить"&&it.visibility==View.VISIBLE}==2)
    prefs.edit().putString("offers",offer(BuildConfig.VERSION_CODE)).commit()
    views(a.window.decorView).filterIsInstance<Button>().first{it.text=="VPN"}.performClick()
    assertFalse(views(a.window.decorView).filterIsInstance<Button>().any{it.text=="Обновить"&&it.visibility==View.VISIBLE})
   }
  }finally{
   val e=prefs.edit().clear();for((k,v)in saved){when(v){is String->e.putString(k,v);is Long->e.putLong(k,v)}};e.commit()
  }
 }
}
