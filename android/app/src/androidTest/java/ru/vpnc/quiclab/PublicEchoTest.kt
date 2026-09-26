package ru.vpnc.quiclab

import android.content.Intent
import android.view.View
import android.view.ViewGroup
import android.widget.*
import androidx.test.platform.app.InstrumentationRegistry
import androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry
import androidx.test.runner.lifecycle.Stage
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

class PublicEchoTest {
    @Test
    fun anonymousAwgSession() {
        val inst = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("public_echo") == "true")
        val host = args.getString("host") ?: error("host required")
        val events = java.util.concurrent.LinkedBlockingQueue<org.json.JSONObject>()
        val s =
            PublicAwgEchoSession(inst.targetContext, "https://$host/lab/echo/awg") {
                events.offer(it)
            }
        try {
            s.move(VpnSession.WIFI)
            val seen = mutableSetOf<String>()
            val until = System.currentTimeMillis() + 30000
            while (System.currentTimeMillis() < until && seen.size < 2) {
                val e = events.poll(1, java.util.concurrent.TimeUnit.SECONDS) ?: continue
                val kind = e.optString("event")
                if (kind in listOf("echo", "transit_echo")) seen.add(kind)
                if (kind == "operation_failed") fail(e.optString("detail"))
            }
            assertEquals(setOf("echo", "transit_echo"), seen)
            assertFalse(LabVpnService.active)
        } finally {
            s.close()
        }
    }

    @Test
    fun publicThreeTransportsWithoutIdentity() {
        val inst = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("public_echo_ui") == "true")
        val c = inst.targetContext
        val host = args.getString("host") ?: error("host required")
        val p = c.getSharedPreferences("server", 0)
        assertEquals("Use the configured public lab", host, p.getString("hostname", ""))
        p.edit().putString("awg_enroll_url", "https://$host/lab/echo/awg").commit()
        c.startActivity(Intent(c, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        Thread.sleep(1800)
        fun views(v: View): List<View> =
            listOf(v) +
                (if (v is ViewGroup) (0 until v.childCount).flatMap { views(v.getChildAt(it)) }
                else emptyList())
        fun activity() =
            ActivityLifecycleMonitorRegistry.getInstance()
                .getActivitiesInStage(Stage.RESUMED)
                .filterIsInstance<MainActivity>()
                .first()
        try {
            inst.runOnMainSync {
                val v = views(activity().window.decorView)
                v.filterIsInstance<CheckBox>()
                    .first { it.text.startsWith("Echo через VPN-профиль") }
                    .isChecked = false
                v.filterIsInstance<CheckBox>()
                    .first { it.text.startsWith("Сравнивать с HTTPS") }
                    .isChecked = true
                v.filterIsInstance<Button>().first { it.text == "Start Echo" }.performClick()
            }
            val until = System.currentTimeMillis() + 45000
            var pairs = 0
            while (System.currentTimeMillis() < until && pairs < 3) {
                Thread.sleep(500)
                inst.runOnMainSync {
                    pairs =
                        views(activity().window.decorView).filterIsInstance<TextView>().count {
                            it.isShown && it.text.toString().matches(Regex("[0-9]+ / [0-9]+"))
                        }
                }
            }
            assertEquals("Three public RTT pairs", 3, pairs)
            assertFalse(LabVpnService.active)
            Thread.sleep(3000)
            inst.runOnMainSync {
                val v = views(activity().window.decorView)
                val label = v.filterIsInstance<TextView>().first { it.text == "QUIC" }
                val xy = IntArray(2)
                label.getLocationOnScreen(xy)
                v.filterIsInstance<ScrollView>().first().scrollBy(0, xy[1] - 140)
            }
            Thread.sleep(500)
            inst.uiAutomation.takeScreenshot()?.let { img ->
                java.io.File(c.filesDir, "public-echo-ui.png").outputStream().use {
                    img.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it)
                }
                img.recycle()
            }
        } finally {
            inst.runOnMainSync {
                ActivityLifecycleMonitorRegistry.getInstance()
                    .getActivitiesInStage(Stage.RESUMED)
                    .filterIsInstance<MainActivity>()
                    .firstOrNull()
                    ?.let { a ->
                        views(a.window.decorView)
                            .filterIsInstance<Button>()
                            .firstOrNull { it.text == "Stop Echo" }
                            ?.performClick()
                    }
            }
        }
    }
}
