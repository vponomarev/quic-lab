package ru.vpnc.quiclab

import android.app.NotificationManager
import android.content.Intent
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test

class AppServiceNotificationTest {
 @Test fun tagsFollowEachRunningFunction() {
  assertEquals("WiFi · AWG",AppServiceNotification.title("WiFi · AWG",false,false))
  assertEquals("[MDM] WiFi · AWG",AppServiceNotification.title("WiFi · AWG",true,false))
  assertEquals("[GEO] WiFi · AWG",AppServiceNotification.title("WiFi · AWG",false,true))
  assertEquals("[MDM][GEO] WiFi · AWG",AppServiceNotification.title("WiFi · AWG",true,true))
 }
 @Test fun standaloneMdmUsesSharedNotificationWithoutActionsAndRemovesItOnStop() {
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val nm=c.getSystemService(NotificationManager::class.java)
  MdmService.owner={object:MdmSession{
   override fun sync(){Thread.sleep(200)}
   override fun pause(){}
   override fun close(){}
  }}
  try {
   MdmService.start(c)
   val deadline=System.currentTimeMillis()+5000
   while(nm.activeNotifications.none{it.id==42} && System.currentTimeMillis()<deadline)Thread.sleep(50)
   val notices=nm.activeNotifications
   assertEquals(1,notices.size)
   val n=notices.single().notification
   assertEquals("[MDM] VPN выключен",n.extras.getString("android.title"))
   assertTrue(n.actions.isNullOrEmpty())
   assertEquals("ru.vpnc.quiclab",n.contentIntent.creatorPackage)
  } finally {
   c.stopService(Intent(c,MdmService::class.java))
   Thread.sleep(500)
   MdmService.owner=null
  }
  assertTrue(nm.activeNotifications.isEmpty())
 }
 @Test fun sharedNotificationSurvivesEitherServiceStopping() {
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val nm=c.getSystemService(NotificationManager::class.java)
  fun waitFor(check:()->Boolean){val until=System.currentTimeMillis()+8000;while(!check() && System.currentTimeMillis()<until)Thread.sleep(100);assertTrue(check())}
  fun text()=nm.activeNotifications.singleOrNull()?.notification?.extras?.getString("android.title").orEmpty()
  MdmService.owner={object:MdmSession{
   override fun sync(){Thread.sleep(200)}
   override fun pause(){}
   override fun close(){}
  }}
  try {
   c.startForegroundService(Intent(c,LabVpnService::class.java))
   waitFor{LabVpnService.active}
   MdmService.start(c)
   waitFor{text().startsWith("[MDM]")}
   assertTrue(LabVpnService.active)
   assertEquals(listOf(42),nm.activeNotifications.map{it.id})
   c.startService(Intent(c,LabVpnService::class.java).setAction("stop"))
   waitFor{text()=="[MDM] VPN выключен"}
   Thread.sleep(500)
   assertEquals("[MDM] VPN выключен",text())
   c.startForegroundService(Intent(c,LabVpnService::class.java))
   waitFor{LabVpnService.active && text().startsWith("[MDM]") && !text().contains("VPN выключен")}
   MdmService.stop(c)
   waitFor{text().isNotEmpty() && !text().startsWith("[MDM]")}
   assertTrue(LabVpnService.active)
   assertEquals(listOf(42),nm.activeNotifications.map{it.id})
  } finally {
   MdmService.stop(c)
   c.startService(Intent(c,LabVpnService::class.java).setAction("stop"))
   Thread.sleep(700)
   MdmService.owner=null
  }
  waitFor{nm.activeNotifications.isEmpty()}
 }

}
