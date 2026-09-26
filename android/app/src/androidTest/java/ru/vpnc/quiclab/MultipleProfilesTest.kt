package ru.vpnc.quiclab

import android.content.Context
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test

class MultipleProfilesTest {
 @Test fun independentSelectionPriorityAndGlobalApps() {
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  assertFalse("Stop VPN before profile test",LabVpnService.active)
  val meta=c.getSharedPreferences("vpn_profiles",Context.MODE_PRIVATE)
  val saved=meta.all.toMap()
  val created=mutableListOf<String>()
  try {
   val a=VpnProfiles.create(c,"Multiple test A");created.add(a.id)
   val b=VpnProfiles.create(c,"Multiple test B");created.add(b.id)
   VpnProfiles.setGlobalApps(c,setOf("app.global"))
   VpnProfiles.preferences(c,b.id).edit().putBoolean("global_apps",false).putStringSet("apps",setOf("app.private")).commit()
   assertEquals(setOf("app.global"),VpnProfiles.apps(c,a.id))
   assertEquals(setOf("app.private"),VpnProfiles.apps(c,b.id))
   VpnProfiles.setEnabled(c,setOf(a.id,b.id));VpnProfiles.setMultiple(c,true);VpnProfiles.setDnsProfile(c,a.id)
   VpnProfiles.move(c,b.id,-1)
   assertTrue(VpnProfiles.list(c).indexOf(b)<VpnProfiles.list(c).indexOf(a))
   assertEquals(setOf(a.id,b.id),VpnProfiles.enabled(c))
   assertEquals(a.id,VpnProfiles.dnsProfile(c))
   assertTrue(VpnProfiles.multiple(c))
   assertEquals(b.id,VpnProfiles.current(c).id)
  } finally {
   created.forEach{VpnProfiles.preferences(c,it).edit().clear().commit()}
   val edit=meta.edit().clear()
   saved.forEach{(k,v)->when(v){is String->edit.putString(k,v);is Boolean->edit.putBoolean(k,v);is Int->edit.putInt(k,v);is Long->edit.putLong(k,v);is Float->edit.putFloat(k,v);is Set<*>->edit.putStringSet(k,v.filterIsInstance<String>().toSet())}}
   check(edit.commit())
  }
 }
}
