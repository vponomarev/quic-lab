package ru.vpnc.quiclab

import android.net.ConnectivityManager
import android.net.Network
import android.os.ParcelFileDescriptor
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit

class HttpsReturnTest {
    @Test fun returnsToWifiWithoutAnyQuicConnection() {
        val inst = InstrumentationRegistry.getInstrumentation()
        val events = LinkedBlockingQueue<JSONObject>()
        val networks = ConcurrentHashMap<Int, Network>()
        val ws = WebSocketSession { events.offer(it) }
        val watcher = QuicSession(inst.targetContext.getSystemService(ConnectivityManager::class.java),
            availability = { net, kind, present ->
                if (present) networks[kind] = net else networks.remove(kind, net)
                ws.availability(net, kind, present)
            }, validation = { net, kind, valid -> ws.validation(net, kind, valid) }) { }
        fun wifi(on: Boolean) {
            inst.uiAutomation.executeShellCommand("svc wifi ${if (on) "enable" else "disable"}").use {
                ParcelFileDescriptor.AutoCloseInputStream(it).readBytes()
            }
        }
        var active = ""
        val ids = linkedSetOf<String>()
        fun waitEchoOn(label: String) {
            val until = System.nanoTime() + TimeUnit.SECONDS.toNanos(35)
            while (System.nanoTime() < until) {
                val e = events.poll(1, TimeUnit.SECONDS) ?: continue
                if (e.optString("event") == "active_network") active = e.optString("detail")
                if (e.optString("event") == "echo") {
                    ids.add(e.getString("connection_id"))
                    if (active == label) return
                }
            }
            error("No HTTPS echo on $label; current=$active sessions=$ids")
        }
        try {
            val until = System.nanoTime() + TimeUnit.SECONDS.toNanos(15)
            while (networks.size < 2 && System.nanoTime() < until) Thread.sleep(100)
            assertEquals("Wi-Fi and cellular are required", 2, networks.size)
            ws.enable(TestConfig.host)
            ws.select(checkNotNull(networks[QuicSession.WIFI]), QuicSession.WIFI)
            waitEchoOn("Wi-Fi")
            wifi(false)
            waitEchoOn("LTE/Cellular")
            wifi(true)
            waitEchoOn("Wi-Fi")
            assertEquals("Three real HTTPS server sessions", 3, ids.size)
        } finally { wifi(true); ws.close(); watcher.close() }
    }
}
