package ru.vpnc.quiclab

import android.app.Notification
import android.app.NotificationManager
import android.content.Intent
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

class VpnNotificationTest {
    @Test fun liveNotificationAndStop() {
        val inst = InstrumentationRegistry.getInstrumentation()
        assumeTrue(InstrumentationRegistry.getArguments().getString("vpn_notification") == "true")
        val c = inst.targetContext
        assertNull("VPN consent required", android.net.VpnService.prepare(c))
        inst.uiAutomation.executeShellCommand("am start -W -n ru.vpnc.quiclab/.MainActivity").use {
            android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()
        }
        val manager = c.getSystemService(NotificationManager::class.java)
        try {
            c.startForegroundService(Intent(c, LabVpnService::class.java))
            val until = System.currentTimeMillis() + 25000
            var n: Notification? = null
            while(System.currentTimeMillis() < until) {
                Thread.sleep(500)
                n = manager.activeNotifications.firstOrNull { it.id == 42 }?.notification
                if(n?.extras?.getCharSequence(Notification.EXTRA_TEXT)?.contains("RTT ") == true && LabVpnService.lastEcho > 0) break
            }
            val notification = requireNotNull(n)
            val title = notification.extras.getCharSequence(Notification.EXTRA_TITLE).toString()
            val text = notification.extras.getCharSequence(Notification.EXTRA_TEXT).toString()
            val details = notification.extras.getCharSequence(Notification.EXTRA_BIG_TEXT).toString()
            assertTrue(title, title.contains(" · "))
            assertTrue(text, text.contains("↑") && text.contains("↓") && text.contains("RTT"))
            assertTrue(details, details.contains("Всего"))
            assertTrue("Fresh tunnel RTT", LabVpnService.lastEcho > 0)
            println("VPN notification: $title | $text | $details")
            notification.actions.first().actionIntent.send()
            val stopUntil = System.currentTimeMillis() + 5000
            while(System.currentTimeMillis() < stopUntil && (LabVpnService.active || manager.activeNotifications.any { it.id == 42 })) Thread.sleep(100)
            assertFalse(LabVpnService.active)
            assertFalse(manager.activeNotifications.any { it.id == 42 })
            Thread.sleep(2500)
            assertFalse("Notification must not reappear", manager.activeNotifications.any { it.id == 42 })
        } finally {
            c.startService(Intent(c, LabVpnService::class.java).setAction("stop"))
        }
    }
}
