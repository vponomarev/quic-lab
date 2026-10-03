package ru.vpnc.quiclab

import android.content.Context
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class MultipleVpnPlanTest {
    @Test fun everyExitProbesItsOwnIPv4IncludingSubnetExit() {
        val c = InstrumentationRegistry.getInstrumentation().targetContext
        assertFalse("Stop VPN before profile test", LabVpnService.active)
        val meta = c.getSharedPreferences("vpn_profiles", Context.MODE_PRIVATE)
        val saved = meta.all.toMap()
        val snapshot = File(c.filesDir, "vpn-configuration.json")
        val previous = if (snapshot.exists()) snapshot.readBytes() else null
        val created = mutableListOf<String>()
        try {
            val internet = VpnProfiles.create(c, "IP test Internet").also { created.add(it.id) }
            val home = VpnProfiles.create(c, "IP test Home").also { created.add(it.id) }
            for ((index, profile) in listOf(internet, home).withIndex()) {
                VpnIdentity.writeBundle(c, profile.id, JSONObject())
                check(VpnProfiles.preferences(c, profile.id).edit()
                    .putString("endpoint", "192.0.2.${10 + index}:443")
                    .putBoolean("demux_enabled", false)
                    .putInt("mode", if (profile == home) 3 else 0)
                    .putString("routes", if (profile == home) "192.168.1.0/24" else "")
                    .commit())
            }
            VpnProfiles.setEnabled(c, setOf(internet.id, home.id))
            VpnProfiles.setMultiple(c, true)
            VpnProfiles.setDnsProfile(c, internet.id)
            val plan = MultipleVpnPlan.load(c)
            assertEquals(setOf(internet.id, home.id), plan.profiles.map { it.id }.toSet())
            plan.profiles.forEach {
                assertTrue("${it.name}: enable its own Gateway IPv4 query", it.config.getBoolean("probe_exit_ip"))
            }
        } finally {
            created.forEach {
                VpnProfiles.preferences(c, it).edit().clear().commit()
                VpnProfiles.identityFile(c, it).delete()
            }
            val edit = meta.edit().clear()
            saved.forEach { (key, value) -> when (value) {
                is String -> edit.putString(key, value)
                is Boolean -> edit.putBoolean(key, value)
                is Int -> edit.putInt(key, value)
                is Long -> edit.putLong(key, value)
                is Float -> edit.putFloat(key, value)
                is Set<*> -> edit.putStringSet(key, value.filterIsInstance<String>().toSet())
            } }
            check(edit.commit())
            if (previous != null) snapshot.writeBytes(previous) else snapshot.delete()
        }
    }
}
