package ru.vpnc.quiclab

import android.view.View
import android.view.ViewGroup
import android.widget.Button
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

class MdmPauseDialogTest {
 @Test fun cancelPausePreservesActiveManagementAndQueuedSample(){
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext;val store=MdmStore(c)
  assumeTrue("Dedicated unbound phone required",store.read().binding==null && !store.read().pendingEnrollment)
  val saved=JSONObject(store.document().toString());val queue=MdmTelemetryStore(c)
  assumeTrue("Do not touch an existing radio queue",queue.pending(System.currentTimeMillis()).length()==0)
  inst.uiAutomation.executeShellCommand("am start -W -n ru.vpnc.quiclab/.MdmEnrollActivity")
   .use{android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()}
  var opened:MdmEnrollActivity?=null
  repeat(40){
   inst.runOnMainSync{opened=androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry.getInstance()
    .getActivitiesInStage(androidx.test.runner.lifecycle.Stage.RESUMED).filterIsInstance<MdmEnrollActivity>().firstOrNull()}
   if(opened==null)Thread.sleep(100)
  }
  val activity=requireNotNull(opened){"MDM screen did not resume"}
  val id="pause-dialog-"+java.util.UUID.randomUUID()
  try{
   store.edit{it.put("binding",MdmStore.bindingJson(MdmBinding("pause-dialog","https://127.0.0.1:9",1,MdmRights(telemetry=true,geo=true),true)))
    .put("active",true).put("rights",MdmRights(telemetry=true,geo=true).json()).put("radio_manual",true)}
   val now=System.currentTimeMillis()
   queue.append(JSONObject().put("id",id).put("measuredAt",java.time.Instant.ofEpochMilli(now).toString()),now)
   val before=store.read()
   inst.runOnMainSync{
    // Render the controlled state after onResume so no test service/network is started.
    MdmEnrollActivity::class.java.getDeclaredMethod("render").apply{isAccessible=true}.invoke(activity)
    fun all(v:View):List<View> = listOf(v)+(if(v is ViewGroup)(0 until v.childCount).flatMap{all(v.getChildAt(it))}else emptyList())
    all(activity.window.decorView).filterIsInstance<Button>().single{it.text.toString()=="Приостановить MDM"}.performClick()
   }
   inst.waitForIdleSync()
   val root=inst.uiAutomation.rootInActiveWindow
   assertNotNull(root)
   assertTrue(root.findAccessibilityNodeInfosByText("не отправленные измерения").isNotEmpty())
   assertTrue(root.findAccessibilityNodeInfosByText("Сбор истории Wi-Fi и сот остановится").isNotEmpty())
   assertEquals(before,store.read())
   assertEquals(1,queue.pending(now).length())
   val cancel=root.findAccessibilityNodeInfosByText("Отмена").first()
   assertTrue(cancel.performAction(android.view.accessibility.AccessibilityNodeInfo.ACTION_CLICK))
   inst.waitForIdleSync()
   assertEquals(before,store.read());assertTrue(MdmTelemetry.wanted(c))
   assertEquals(id,queue.pending(now).getJSONObject(0).getString("id"))
  }finally{
   inst.runOnMainSync{activity.finish()}
   queue.acknowledge(setOf(id),System.currentTimeMillis())
   store.edit{j->j.keys().asSequence().toList().forEach{j.remove(it)};saved.keys().forEach{k->j.put(k,saved.get(k))}}
  }
 }
}
