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
}
