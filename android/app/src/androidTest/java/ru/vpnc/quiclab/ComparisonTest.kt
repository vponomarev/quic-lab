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
class ComparisonTest {
    @Test fun quicMigratesWhileWssOpensNewSessions() {
        assumeTrue(InstrumentationRegistry.getArguments().getString("comparison") == "true")
        val inst = InstrumentationRegistry.getInstrumentation()
        val events = LinkedBlockingQueue<Pair<Boolean, JSONObject>>()
        val ws = WebSocketSession { events.offer(false to it); if (it.optString("event") != "echo") Log.i("ComparisonTest", "WSS $it") }
        val qs = QuicSession(inst.targetContext.getSystemService(ConnectivityManager::class.java), { net, kind -> ws.select(net, kind) },
            { net, kind, present -> ws.availability(net, kind, present) },
            { net, kind, valid -> ws.validation(net, kind, valid) }) {
            events.offer(true to it); if (it.optString("event") != "echo") Log.i("ComparisonTest", "QUIC $it")
        }
        val ids = arrayOf(linkedSetOf<String>(), linkedSetOf<String>())
        fun waitFor(predicate: (Boolean, JSONObject) -> Boolean) {
            val until = System.nanoTime() + TimeUnit.SECONDS.toNanos(40)
            while (System.nanoTime() < until) {
                val (quic, e) = events.poll(1, TimeUnit.SECONDS) ?: continue
                if (e.optString("event") == "echo") ids[if (quic) 0 else 1].add(e.getString("connection_id"))
                if (quic) check(e.optString("event") !in listOf("disconnected", "session_closed", "operation_failed")) { e.toString() }
                if (predicate(quic, e)) return
            }
            error("Comparison timed out; ids=${ids.toList()}")
        }
        fun wifi(on: Boolean) {
            inst.uiAutomation.executeShellCommand("svc wifi ${if (on) "enable" else "disable"}").use {
                ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()
            }
        }
        var toggled = false
        try {
            val ready = mutableSetOf<String>()
            waitFor { quic, e ->
                if (quic && e.optString("event") == "network_validated") ready.add(e.getString("detail"))
                ready.size >= 2
            }
            ws.enable(TestConfig.host)
            qs.startOrMigrate(QuicSession.WIFI, TestConfig.endpoint, TestConfig.host, "", 50)
            waitFor { _, _ -> ids.all { it.size == 1 } }
            waitFor { quic, e -> quic && e.optString("event") == "standby_ready" }
            val idleSeconds = InstrumentationRegistry.getArguments().getString("initialDwell")?.toLong() ?: 0L
            val idleUntil = System.nanoTime() + TimeUnit.SECONDS.toNanos(idleSeconds)
            while (System.nanoTime() < idleUntil) {
                val step = minOf(idleUntil, System.nanoTime() + TimeUnit.SECONDS.toNanos(20))
                waitFor { quic, e -> quic && e.optString("event") == "echo" && System.nanoTime() >= step }
            }
            val cycles = InstrumentationRegistry.getArguments().getString("cycles")?.toInt() ?: 1
            for ((index, on) in List(cycles) { listOf(false, true) }.flatten().withIndex()) {
                toggled = true
                val independentWss = index == 0 && InstrumentationRegistry.getArguments().getString("independentWss") == "true"
                if (independentWss) qs.setAutomatic(false)
                wifi(on)
                if (independentWss) {
                    waitFor { _, _ -> ids[1].size >= 2 }
                    Log.i("ComparisonTest", "WSS recovered with QUIC migration disabled")
                    qs.setAutomatic(true)
                }
                waitFor { quic, e -> quic && e.optString("event") == "active_network" && e.optString("detail") == if (on) "Wi-Fi" else "LTE/Cellular" }
                waitFor { _, _ -> ids[1].size >= index + 2 }
                assertEquals("QUIC must keep one server identity", 1, ids[0].size)
                Log.i("ComparisonTest", "WIFI=$on quic_ids=${ids[0]} wss_ids=${ids[1]}")
                val dwell = InstrumentationRegistry.getArguments().getString("dwell")?.toLong() ?: 0L
                if (dwell > 0) {
                    val until = System.nanoTime() + TimeUnit.SECONDS.toNanos(dwell)
                    waitFor { _, _ -> System.nanoTime() >= until }
                }
            }
        } finally { if (toggled) wifi(true); qs.close(); ws.close() }
    }
}
