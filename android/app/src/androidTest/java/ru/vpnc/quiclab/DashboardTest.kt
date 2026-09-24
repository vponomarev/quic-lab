package ru.vpnc.quiclab

import android.os.ParcelFileDescriptor
import android.graphics.Bitmap
import android.os.SystemClock
import android.view.View
import android.view.ViewGroup
import android.widget.Button
import android.widget.TextView
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry
import androidx.test.runner.lifecycle.Stage
import org.junit.Assert.*
import org.junit.Test
import java.io.File

class DashboardTest {
    @Test fun bothCardsShowRealServerSessions() {
        val inst = InstrumentationRegistry.getInstrumentation()
        // Launch as shell: MIUI rejects an instrumentation app's background start.
        inst.uiAutomation.executeShellCommand("am start -W -n ru.vpnc.quiclab/.MainActivity").use {
            ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()
        }
        var launched: MainActivity? = null
        inst.runOnMainSync {
            launched = ActivityLifecycleMonitorRegistry.getInstance()
                .getActivitiesInStage(Stage.RESUMED).filterIsInstance<MainActivity>().firstOrNull()
        }
        val activity = checkNotNull(launched) { "Dashboard must be resumed; unlock the phone before this test" }
        fun texts(view: View): List<TextView> = (if (view is TextView) listOf(view) else emptyList()) +
            (if (view is ViewGroup) (0 until view.childCount).flatMap { texts(view.getChildAt(it)) } else emptyList())
        try {
            Thread.sleep(1500)
            inst.runOnMainSync {
                val fields = texts(activity.window.decorView).filterIsInstance<android.widget.EditText>()
                fields[0].setText(TestConfig.endpoint)
                fields[1].setText(TestConfig.host)
                fields[2].setText("")
                texts(activity.window.decorView).filterIsInstance<Button>().first { it.text == "Начать опыт" }.performClick()
            }
            var ready = false
            val until = SystemClock.elapsedRealtime() + 15000
            while (!ready && SystemClock.elapsedRealtime() < until) {
                inst.runOnMainSync { ready = texts(activity.window.decorView).count { it.text == "Сеансов   1" } >= 2 }
                Thread.sleep(200)
            }
            assertTrue("Both transports must receive a server-generated identity", ready)
            if (InstrumentationRegistry.getArguments().getString("radio") == "true") {
                inst.runOnMainSync {
                    texts(activity.window.decorView).filterIsInstance<Button>().first { it.text == "На мобильную" }.performClick()
                }
                val switchUntil = SystemClock.elapsedRealtime() + 15000
                var switched = false
                while (!switched && SystemClock.elapsedRealtime() < switchUntil) {
                    inst.runOnMainSync { switched = texts(activity.window.decorView).any { it.text == "Сеансов   2" } }
                    Thread.sleep(200)
                }
                assertTrue("WSS reconnect must be visible after a path change", switched)
                Thread.sleep(1500)
            }
            val screenshot = inst.uiAutomation.takeScreenshot()
            File(inst.targetContext.getExternalFilesDir(null), "dashboard.png").outputStream().use {
                screenshot.compress(Bitmap.CompressFormat.PNG, 100, it)
            }
            if (InstrumentationRegistry.getArguments().getString("radio") == "true") {
                inst.runOnMainSync {
                    val box = texts(activity.window.decorView).first { it.text == "Сети и радиоканал" }.parent as View
                    box.requestRectangleOnScreen(android.graphics.Rect(0, 0, box.width, box.height), true)
                }
                Thread.sleep(400)
                File(inst.targetContext.getExternalFilesDir(null), "radio.png").outputStream().use {
                    inst.uiAutomation.takeScreenshot().compress(Bitmap.CompressFormat.PNG, 100, it)
                }
            }
            inst.runOnMainSync {
                texts(activity.window.decorView).filterIsInstance<Button>().first { it.text == "Завершить опыт" }.performClick()
            }
        } finally { inst.runOnMainSync { activity.finish() } }
    }
}
