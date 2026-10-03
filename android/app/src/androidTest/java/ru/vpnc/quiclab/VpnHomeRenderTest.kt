package ru.vpnc.quiclab

import android.content.Intent
import android.os.ParcelFileDescriptor
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.TextView
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry
import androidx.test.runner.lifecycle.Stage
import org.junit.Assert.*
import org.junit.Test

class VpnHomeRenderTest {
    @Test fun vpnHomeHasExitDashboardAndSeparateEchoWithoutStartingVpn() {
        val inst = InstrumentationRegistry.getInstrumentation()
        val wasActive = LabVpnService.active
        inst.uiAutomation.executeShellCommand("am start -W -a android.intent.action.MAIN -c android.intent.category.LAUNCHER -n ru.vpnc.quiclab/.MainActivity").use {
            ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()
        }
        var activity: VpnActivity? = null
        val deadline = System.currentTimeMillis() + 10_000
        while (activity == null && System.currentTimeMillis() < deadline) {
            inst.runOnMainSync {
                activity = ActivityLifecycleMonitorRegistry.getInstance().getActivitiesInStage(Stage.RESUMED).filterIsInstance<VpnActivity>().firstOrNull()
            }
            if (activity == null) Thread.sleep(100)
        }
        assertNotNull("VPN activity must resume within 10 seconds", activity)
        fun views(view: View): List<View> = listOf(view) + if (view is ViewGroup)
            (0 until view.childCount).flatMap { views(view.getChildAt(it)) } else emptyList()
        inst.runOnMainSync {
            val all = views(requireNotNull(activity).window.decorView)
            assertTrue("VPN home exposes per-exit dashboard", all.filterIsInstance<TextView>().any { it.text == "Подключения и трафик" })
            all.filterIsInstance<Button>().first { it.text == "Диагностика" }.performClick()
            assertTrue("Echo remains a separate screen", views(requireNotNull(activity).window.decorView).filterIsInstance<Button>().any { it.text == "Echo · диагностика сетей" })
            assertEquals("Opening home must not start VPN", wasActive, LabVpnService.active)
        }
    }
}
