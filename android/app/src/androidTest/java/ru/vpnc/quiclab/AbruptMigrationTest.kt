package ru.vpnc.quiclab

import android.net.ConnectivityManager
import android.os.ParcelFileDescriptor
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

@RunWith(AndroidJUnit4::class)
class AbruptMigrationTest {
    @Test fun wifiOffAndOnPreservesSession() {
        val args = InstrumentationRegistry.getArguments()
        assumeTrue("Explicit opt-in required: toggles phone Wi-Fi", args.getString("abrupt") == "true")
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val events = LinkedBlockingQueue<JSONObject>()
        var connections = 0
        var maxGap = 0.0
        var maxRTT = 0.0
        val session = QuicSession(instrumentation.targetContext.getSystemService(ConnectivityManager::class.java)) {
            events.offer(it)
            if (it.optString("event") != "echo") Log.i("QuicLabTest", it.toString())
        }
        fun awaitEvent(predicate: (JSONObject) -> Boolean): JSONObject {
            val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(35)
            while (System.nanoTime() < deadline) {
                val e = events.poll(1, TimeUnit.SECONDS) ?: continue
                val kind = e.optString("event")
                if (kind == "connected") connections++
                if (kind == "echo") {
                    maxGap = maxOf(maxGap, e.optDouble("gap_ms"))
                    maxRTT = maxOf(maxRTT, e.optDouble("rtt_ms"))
                }
                check(kind !in listOf("disconnected", "session_closed", "operation_failed", "auto_migration_failed")) { e.toString() }
                if (predicate(e)) return e
            }
            error("Timed out awaiting expected network/echo")
        }
        fun wifi(enabled: Boolean) {
            instrumentation.uiAutomation.executeShellCommand("svc wifi ${if (enabled) "enable" else "disable"}").use {
                ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()
            }
        }
        var toggled = false
        try {
            val ready = mutableSetOf<String>()
            awaitEvent { e ->
                if (e.optString("event") == "network_validated") ready.add(e.getString("detail"))
                ready.containsAll(listOf("Wi-Fi", "LTE/Cellular"))
            }
            session.startOrMigrate(QuicSession.WIFI, args.getString("server") ?: TestConfig.endpoint, TestConfig.host, "", 50L)
            val first = awaitEvent { it.optString("event") == "echo" }
            Log.i("QuicLabTest", "INITIAL $first")
            var previous = first
            val cycles = (args.getString("cycles") ?: "1").toInt().coerceIn(1, 20)
            for (enabled in List(cycles) { listOf(false, true) }.flatten()) {
                if (!enabled) awaitEvent { it.optString("event") == "standby_ready" }
                maxGap = 0.0
                maxRTT = 0.0
                toggled = true
                wifi(enabled)
                awaitEvent { it.optString("event") == "active_network" && it.optString("detail") == if (enabled) "Wi-Fi" else "LTE/Cellular" }
                val next = awaitEvent { it.optString("event") == "echo" && it.optString("peer") != previous.optString("peer") }
                assertEquals(first.getString("connection_id"), next.getString("connection_id"))
                assertEquals(first.getLong("stream_id"), next.getLong("stream_id"))
                assertTrue(next.getLong("seq") > previous.getLong("seq"))
                Log.i("QuicLabTest", "WIFI_ENABLED=$enabled max_gap_ms=$maxGap max_rtt_ms=$maxRTT $next")
                previous = next
            }
            assertEquals("Must not reconnect", 1, connections)
        } finally {
            if (toggled) wifi(true)
            session.close()
        }
    }
}
