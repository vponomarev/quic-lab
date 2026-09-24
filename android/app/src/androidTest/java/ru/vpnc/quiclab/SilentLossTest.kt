package ru.vpnc.quiclab

import android.net.ConnectivityManager
import android.util.Log
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit

/** External harness temporarily drops only this lab's Wi-Fi UDP traffic on server. */
@RunWith(AndroidJUnit4::class)
class SilentLossTest {
    @Test fun stillConnectedWifiWithNoPacketsFailsOver() {
        assumeTrue(InstrumentationRegistry.getArguments().getString("silent") == "true")
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val events = LinkedBlockingQueue<JSONObject>()
        var connected = 0
        var lostWifi = false
        var maxGap = 0.0
        val session = QuicSession(context.getSystemService(ConnectivityManager::class.java)) {
            events.offer(it)
            if (it.optString("event") != "echo") Log.i("QuicLabTest", it.toString())
        }
        fun waitFor(predicate: (JSONObject) -> Boolean): JSONObject {
            val until = System.nanoTime() + TimeUnit.SECONDS.toNanos(50)
            while (System.nanoTime() < until) {
                val e = events.poll(1, TimeUnit.SECONDS) ?: continue
                when (e.optString("event")) {
                    "connected" -> connected++
                    "echo" -> maxGap = maxOf(maxGap, e.optDouble("gap_ms"))
                    "network_lost" -> if (e.optString("detail").startsWith("Wi-Fi")) lostWifi = true
                    "disconnected", "session_closed", "operation_failed", "controller_error" -> error(e.toString())
                }
                if (predicate(e)) return e
            }
            error("Expected event not received; external loss harness must be running")
        }
        try {
            val ready = mutableSetOf<String>()
            waitFor {
                if (it.optString("event") == "network_validated") ready.add(it.getString("detail"))
                ready.containsAll(listOf("Wi-Fi", "LTE/Cellular"))
            }
            session.startOrMigrate(QuicSession.WIFI, TestConfig.endpoint, TestConfig.host, "", 50)
            val first = waitFor { it.optString("event") == "echo" }
            waitFor { it.optString("event") == "standby_ready" }
            Log.i("QuicLabTest", "SILENT_READY $first")
            waitFor { it.optString("event") == "active_network" && it.optString("detail") == "LTE/Cellular" }
            val next = waitFor { it.optString("event") == "echo" && it.optString("peer") != first.getString("peer") }
            assertFalse("Wi-Fi must remain connected during silent loss", lostWifi)
            assertEquals(1, connected)
            assertEquals(first.getString("connection_id"), next.getString("connection_id"))
            assertEquals(first.getLong("stream_id"), next.getLong("stream_id"))
            Log.i("QuicLabTest", "SILENT_RECOVERED max_gap_ms=$maxGap $next")
        } finally { session.close() }
    }
}
