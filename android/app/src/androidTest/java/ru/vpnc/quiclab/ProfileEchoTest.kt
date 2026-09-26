package ru.vpnc.quiclab

import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

class ProfileEchoTest {
    @Test
    fun threeTransportsAndTransitWithoutVpn() {
        val inst = InstrumentationRegistry.getInstrumentation()
        assumeTrue(InstrumentationRegistry.getArguments().getString("profile_echo") == "true")
        val c = inst.targetContext
        assertFalse(LabVpnService.active)
        val events = LinkedBlockingQueue<Pair<String, JSONObject>>()
        val session = ProfileEchoSession(c) { t, e -> events.offer(t to e) }
        try {
            assertEquals(setOf("quic", "https", "awg"), session.transports)
            Thread.sleep(1500)
            session.move(VpnSession.WIFI)
            val replies = mutableSetOf<String>()
            val transit = mutableSetOf<String>()
            val needsTransit =
                !VpnProfiles.preferences(c).getString("transit_endpoint", "").isNullOrBlank()
            val until = SystemClock.elapsedRealtime() + 45000
            while (
                SystemClock.elapsedRealtime() < until &&
                    (replies.size < 3 || (needsTransit && transit.size < 3))
            ) {
                val (t, e) = events.poll(1, TimeUnit.SECONDS) ?: continue
                if (e.optString("event") == "echo") replies.add(t)
                if (e.optString("event") == "transit_echo") transit.add(t)
                if (e.optString("event") in listOf("echo", "transit_echo", "operation_failed"))
                    println("$t $e")
            }
            assertEquals("Local RTT on all transports", session.transports, replies)
            if (needsTransit) assertEquals("End-to-end transit RTT", session.transports, transit)
            assertFalse("Echo must not start a system VPN", LabVpnService.active)
        } finally {
            session.close()
        }
    }

    @Test
    fun dashboardShowsThreeTransports() {
        val inst = InstrumentationRegistry.getInstrumentation()
        assumeTrue(InstrumentationRegistry.getArguments().getString("profile_echo_ui") == "true")
        val c = inst.targetContext
        c.startActivity(
            android.content
                .Intent(c, MainActivity::class.java)
                .addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK)
        )
        Thread.sleep(1500)
        fun views(v: android.view.View): List<android.view.View> =
            listOf(v) +
                (if (v is android.view.ViewGroup)
                    (0 until v.childCount).flatMap { views(v.getChildAt(it)) }
                else emptyList())
        fun activity() =
            androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry.getInstance()
                .getActivitiesInStage(androidx.test.runner.lifecycle.Stage.RESUMED)
                .filterIsInstance<MainActivity>()
                .first()
        try {
            inst.runOnMainSync {
                val v = views(activity().window.decorView)
                v.filterIsInstance<android.widget.CheckBox>()
                    .first { it.text.startsWith("Echo через VPN-профиль") }
                    .isChecked = true
                v.filterIsInstance<android.widget.Button>()
                    .first { it.text == "Start Echo" }
                    .performClick()
            }
            Thread.sleep(7000)
            inst.runOnMainSync {
                val v = views(activity().window.decorView)
                val text =
                    v.filterIsInstance<android.widget.TextView>()
                        .filter { it.isShown }
                        .map { it.text.toString() }
                assertTrue(text.any { it == "AmneziaWG" })
                assertTrue(
                    "Three RTT pairs: $text",
                    text.count { it.matches(Regex("[0-9]+ / [0-9]+")) } >= 3,
                )
                assertFalse(LabVpnService.active)
                val label =
                    v.filterIsInstance<android.widget.TextView>().first { it.text == "QUIC" }
                val location = IntArray(2)
                label.getLocationOnScreen(location)
                v.filterIsInstance<android.widget.ScrollView>()
                    .first()
                    .scrollBy(0, location[1] - 140)
            }
            Thread.sleep(500)
            inst.uiAutomation.takeScreenshot()?.let { img ->
                java.io.File(c.filesDir, "profile-echo-ui.png").outputStream().use {
                    img.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it)
                }
                img.recycle()
            }
        } finally {
            inst.runOnMainSync {
                val current =
                    androidx.test.runner.lifecycle.ActivityLifecycleMonitorRegistry.getInstance()
                        .getActivitiesInStage(androidx.test.runner.lifecycle.Stage.RESUMED)
                        .filterIsInstance<MainActivity>()
                        .firstOrNull()
                current?.let {
                    views(it.window.decorView)
                        .filterIsInstance<android.widget.Button>()
                        .firstOrNull { it.text == "Stop Echo" }
                        ?.performClick()
                }
            }
        }
    }
}
