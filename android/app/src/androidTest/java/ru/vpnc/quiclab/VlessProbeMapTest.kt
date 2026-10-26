package ru.vpnc.quiclab

import android.content.Intent
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.Spinner
import android.widget.TextView
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class VlessProbeMapTest {
 private fun views(v:View):List<View> = listOf(v)+(if(v is ViewGroup)(0 until v.childCount).flatMap{views(v.getChildAt(it))}else emptyList())
 @Test fun previewShowsIndependentNetworkMapWithoutEnablingActions(){
  val inst=InstrumentationRegistry.getInstrumentation();val c=inst.targetContext
  inst.uiAutomation.executeShellCommand("am start -W -n ru.vpnc.quiclab/.MainActivity").use{android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()}
  val rows=JSONArray()
  for(i in 1..12)rows.put(JSONObject().put("id","node$i").put("name","Сервер $i").put("endpoint","node$i.example:443").put("serverName","proxy.example").put("transport","ws").put("haConfigured",true).put("outcome",when(i){3->"relay_failed";5->"dns";8->"queued";else->"success"}).put("relayOutcome",when(i){3->"relay_auth";5,8->"not_attempted";else->"success"}).put("echoRttMs",20+i*11).put("echoSucceeded",3))
  val run=JSONObject().put("runId","preview").put("state","finished").put("network","wifi").put("startedAt","2026-10-23T12:00:00Z").put("phases",JSONObject().put("wifi",JSONObject().put("state","finished").put("rows",rows)).put("cell",JSONObject().put("state","skipped").put("reason","budget").put("rows",JSONArray())))
  val activity=inst.startActivitySync(Intent(c,VlessProbeActivity::class.java).putExtra("probe_preview",run.toString()).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
  try {
   inst.runOnMainSync{
    val all=views(activity.window.decorView)
    assertTrue(all.filterIsInstance<TextView>().any{it.text.toString().contains("Карта доступности")})
    assertTrue(all.filterIsInstance<Button>().filter{it.text.toString() in listOf("Проверить все","Неудачные","Стоп")}.all{!it.isEnabled})
    assertTrue(all.any{it.contentDescription?.toString()?.contains("Сервер 3") == true})
    val networks=all.filterIsInstance<Spinner>().single{it.tag=="probe_network"}
    assertEquals(2,networks.adapter.count)
   }
   if(InstrumentationRegistry.getArguments().getString("probe_map_ui")=="true"){
    android.os.SystemClock.sleep(600)
    val screenshot=inst.uiAutomation.takeScreenshot()
    java.io.File(c.cacheDir,"probe-map-ui.png").outputStream().use{screenshot.compress(android.graphics.Bitmap.CompressFormat.PNG,100,it)}
    screenshot.recycle()
   }
   inst.runOnMainSync{
    val network=views(activity.window.decorView).filterIsInstance<Spinner>().single{it.tag=="probe_network"}
    network.setSelection(1)
   }
   inst.waitForIdleSync()
   inst.runOnMainSync{
    assertTrue(views(activity.window.decorView).filterIsInstance<TextView>().any{it.text.toString().contains("Лимит LTE")})
   }
  } finally {inst.runOnMainSync{activity.finish()}}
 }
}
