package ru.vpnc.quiclab

import android.os.ParcelFileDescriptor
import android.view.View
import android.view.ViewGroup
import android.widget.*
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry
import androidx.test.runner.lifecycle.Stage
import org.junit.Assert.*
import org.junit.Test

class AppSelectionTest {
 @Test fun searchAndKeepSelectionAcrossFilters() {
  val inst=InstrumentationRegistry.getInstrumentation()
  inst.uiAutomation.executeShellCommand("am start -W -n ru.vpnc.quiclab/.MainActivity").use { ParcelFileDescriptor.AutoCloseInputStream(it).readBytes() }
  fun views(v:View):List<View> = listOf(v)+(if(v is ViewGroup)(0 until v.childCount).flatMap{views(v.getChildAt(it))}else emptyList())
  fun button(title:String) {
   inst.runOnMainSync {
    val a=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).first()
    views(a.window.decorView).filterIsInstance<Button>().first{it.text==title}.performClick()
   }
  }
  Thread.sleep(2500)
  button("VPN / Exit node")
  Thread.sleep(1000)
  button("Выбрать приложения")
  var picker:AppSelectionActivity?=null
  val deadline=System.currentTimeMillis()+15000
  var ready=false
  while(!ready && System.currentTimeMillis()<deadline) {
   inst.runOnMainSync {
    picker=ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<AppSelectionActivity>().firstOrNull()
    ready=picker?.let{views(it.window.decorView).filterIsInstance<ListView>().first().count>0} ?: false
   }
   Thread.sleep(100)
  }
  assertTrue("App list loads",ready)
  inst.runOnMainSync {
   val all=views(picker!!.window.decorView)
   val search=all.filterIsInstance<EditText>().first()
   val list=all.filterIsInstance<ListView>().first()
   search.setText("Chrome")
   assertTrue("Search by human name",list.count>0)
   val first=list.adapter.getView(0,null,list)
   val before=views(first).filterIsInstance<CheckBox>().first().isChecked
   val labels=views(first).filterIsInstance<TextView>().map{it.text.toString()}
   assertTrue("Human-readable label",labels.any{it.contains("Chrome",true) && !it.startsWith("com.")})
   list.performItemClick(first,0,0)
   search.setText("no-such-app-test-924")
   assertEquals(0,list.count)
   search.setText("Chrome")
   val after=list.adapter.getView(0,null,list)
   assertEquals(!before,views(after).filterIsInstance<CheckBox>().first().isChecked)
  }
  button("Отмена")
  Thread.sleep(500)
  inst.runOnMainSync {
   assertTrue(ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).any{it is VpnActivity})
  }
 }
}
