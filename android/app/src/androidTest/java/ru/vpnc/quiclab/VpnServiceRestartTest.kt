package ru.vpnc.quiclab

import android.content.Intent
import android.os.ParcelFileDescriptor
import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test

/** Real ActivityManager restart policy, not a mocked service return value. */
class VpnServiceRestartTest {
    @Test fun activeServiceStaysStickyAndExplicitStopWins() {
        val inst = InstrumentationRegistry.getInstrumentation()
        val c = inst.targetContext
        fun shell(command: String): String = inst.uiAutomation.executeShellCommand(command).use {
            ParcelFileDescriptor.AutoCloseInputStream(it).readBytes().toString(Charsets.UTF_8)
        }
        fun waitFor(label: String, condition: () -> Boolean) {
            val until = SystemClock.elapsedRealtime() + 30000
            while (!condition() && SystemClock.elapsedRealtime() < until) Thread.sleep(100)
            assertTrue("$label: ${LabVpnService.status}", condition())
        }
        fun serviceDump() = shell("dumpsys activity services ru.vpnc.quiclab")
        fun assertSticky() {
            val dump = serviceDump()
            assertTrue(dump, dump.contains("startRequested=true"))
            assertTrue("Active VPN must request restart after process death: $dump", dump.contains("stopIfKilled=false"))
        }
        assertNull("VPN consent required", android.net.VpnService.prepare(c))
        try {
            c.stopService(Intent(c, LabVpnService::class.java))
            waitFor("initial stop") { !LabVpnService.active }
            Thread.sleep(500)
            shell("am start -n ru.vpnc.quiclab/.MainActivity")
            c.startForegroundService(Intent(c, LabVpnService::class.java))
            waitFor("VPN active") { LabVpnService.active }
            Thread.sleep(500)
            assertSticky()
            // Auxiliary commands must not revoke the restart policy of a running VPN.
            for (action in listOf("exit-ip", "move", "toggle-profile")) {
                c.startService(Intent(c, LabVpnService::class.java).setAction(action))
                Thread.sleep(350)
                assertSticky()
            }
            c.startService(Intent(c, LabVpnService::class.java))
            Thread.sleep(350)
            assertSticky()
            c.startService(Intent(c, LabVpnService::class.java).setAction("stop"))
            waitFor("explicit stop") { !LabVpnService.active }
            Thread.sleep(2000)
            assertFalse("Stopped VPN must not remain started", serviceDump().contains("startRequested=true"))
            assertFalse(LabVpnService.active)
        } finally {
            c.stopService(Intent(c, LabVpnService::class.java))
        }
    }
}