package ru.vpnc.quiclab
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test
class VpnProfilesTest {
 @Test fun profilesKeepIndependentSettingsAndShareOnlyGlobalApps() {
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  org.junit.Assume.assumeFalse(LabVpnService.active)
  val previous=VpnProfiles.current(c).id
  val global=VpnProfiles.globalApps(c)
  val made=mutableListOf<String>()
  try {
   val a=VpnProfiles.create(c,"Test home");made.add(a.id)
   VpnProfiles.preferences(c).edit().putString("endpoint","home.example:4434").putBoolean("global_apps",false).putStringSet("apps",setOf("home.app")).commit()
   VpnProfiles.identityFile(c).writeBytes(byteArrayOf(1,2,3))
   val b=VpnProfiles.create(c,"Test work");made.add(b.id)
   VpnProfiles.preferences(c).edit().putString("endpoint","work.example:4434").commit()
   VpnProfiles.setGlobalApps(c,setOf("shared.app"))
   assertEquals(setOf("shared.app"),VpnProfiles.apps(c))
   assertFalse(VpnProfiles.identityFile(c).exists())
   VpnProfiles.select(c,a.id)
   assertEquals("home.example:4434",VpnProfiles.preferences(c).getString("endpoint",null))
   assertEquals(setOf("home.app"),VpnProfiles.apps(c))
   assertArrayEquals(byteArrayOf(1,2,3),VpnProfiles.identityFile(c).readBytes())
   VpnProfiles.preferences(c).edit().putBoolean("global_apps",true).commit()
   assertEquals(setOf("shared.app"),VpnProfiles.apps(c))
   VpnProfiles.setGlobalApps(c,setOf("new.shared.app"))
   VpnProfiles.select(c,b.id)
   assertEquals(setOf("new.shared.app"),VpnProfiles.apps(c))
   VpnProfiles.select(c,a.id)
   VpnProfiles.preferences(c).edit().putBoolean("global_apps",false).commit()
   assertEquals(setOf("home.app"),VpnProfiles.apps(c))
   val cert=VpnProfiles.identityFile(c)
   VpnProfiles.delete(c,a.id);made.remove(a.id)
   assertFalse(cert.exists())
   assertEquals("work.example:4434",VpnProfiles.preferences(c,b.id).getString("endpoint",null))
  } finally {
   VpnProfiles.select(c,previous)
   made.forEach{VpnProfiles.delete(c,it)}
   VpnProfiles.setGlobalApps(c,global)
  }
 }
}
