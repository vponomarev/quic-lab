package ru.vpnc.quiclab

import android.content.Intent
import android.net.VpnService
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.io.File

class MultiprotocolTest {
    @Test fun oneProfileThreeTransports() {
        val inst = InstrumentationRegistry.getInstrumentation()
        assumeTrue(InstrumentationRegistry.getArguments().getString("multiprotocol_live") == "true")
        val context = inst.targetContext
        val original = VpnProfiles.current(context).id
        val source = File(context.filesDir, "multiprotocol-test.json")
        val profile = JSONObject(source.readText())
        source.delete()
        assertFalse("Stop active VPN before testing", LabVpnService.active)
        fun waitFor(label: String, seconds: Int = 40, predicate: () -> Boolean) {
            val until = System.currentTimeMillis() + seconds * 1000
            while (System.currentTimeMillis() < until && !predicate()) Thread.sleep(100)
            assertTrue("$label: ${LabVpnService.status}", predicate())
        }
        try {
            ProfileImport.save(context, profile)
            assertEquals(setOf("quic", "https", "awg"), VpnProfiles.preferences(context).getStringSet("available_transports", emptySet()))
            assertTrue(VpnIdentity.load(context).has("certificate"))
            assertTrue(VpnIdentity.load(context).has("awg_config"))
            assertFalse(VpnProfiles.preferences(context).all.toString().contains("PrivateKey"))
            for (transport in listOf("quic", "https", "awg")) {
                val prefs = VpnProfiles.preferences(context)
                prefs.edit().putString("transport", transport).putString("endpoint", prefs.getString("${transport}_endpoint", "")).commit()
                context.startActivity(Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                Thread.sleep(1200)
                VpnService.prepare(context)?.let {
                    context.startActivity(it.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                    waitFor("VPN permission", 90) { VpnService.prepare(context) == null }
                }
                context.startForegroundService(Intent(context, LabVpnService::class.java))
                waitFor("$transport RTT") { LabVpnService.active && LabVpnService.rtt > 0 && LabVpnService.lastEcho >= LabVpnService.startedAt }
                if (profile.optString("transit_endpoint").isNotBlank()) waitFor("$transport transit RTT") { LabVpnService.lastTransitEcho >= LabVpnService.startedAt && LabVpnService.transitRtt > 0 }
                waitFor("$transport exit IP") { LabVpnService.exitIP.isNotBlank() }
                if (InstrumentationRegistry.getArguments().getString("capture_transit_ui") == "true") {
                    inst.uiAutomation.executeShellCommand("am start -W -n ru.vpnc.quiclab/.MainActivity").close()
                    Thread.sleep(1500)
                    inst.uiAutomation.takeScreenshot()?.let { screenshot ->
                        File(context.filesDir, "transit-ui-$transport.png").outputStream().use { screenshot.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it) }
                        screenshot.recycle()
                    }
                }
                context.startService(Intent(context, LabVpnService::class.java).setAction("stop"))
                waitFor("$transport stopped") { !LabVpnService.active }
                Thread.sleep(1500)
            }
        } finally {
            context.startService(Intent(context, LabVpnService::class.java).setAction("stop"))
            Thread.sleep(2000)
            if (VpnProfiles.current(context).id != original) VpnProfiles.delete(context)
            VpnProfiles.select(context, original)
        }
    }
}
