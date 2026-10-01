package ru.vpnc.quiclab

import mobile.Mobile
import mobile.SocketBinder
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.net.DatagramPacket
import java.net.DatagramSocket
import java.net.InetAddress
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

class TrafficBudgetTest {
    @Test fun socketBudgetStopsCellButAllowsWifiAndNewRun() {
        val run = VpnBudgetRun()
        val budget = run.start(4096)
        val calls = AtomicInteger()
        val raw = object : SocketBinder { override fun bind(fd: Long) { calls.incrementAndGet() } }
        val client = Mobile.newClient(null)
        val worker = Executors.newSingleThreadExecutor()
        DatagramSocket(0, InetAddress.getByName("127.0.0.1")).use { server ->
            server.soTimeout = 5000
            val task = worker.submit {
                try { client.start("127.0.0.1:${server.localPort}", "localhost", "", 100, budget.bind(raw,"cell")) }
                catch (_: Exception) { /* Loopback peer is deliberately not a QUIC server. */ }
            }
            try {
                val initial = DatagramPacket(ByteArray(65535),65535)
                server.receive(initial)
                // Even malformed inbound datagrams consume physical socket bytes.
                repeat(8) { server.send(DatagramPacket(ByteArray(8192),8192,initial.socketAddress)) }
                task.get(15, TimeUnit.SECONDS)
                val snapshot = JSONObject(budget.snapshot())
                assertTrue(snapshot.getLong("used") >= 4096)
                assertTrue(snapshot.getBoolean("blocked"))
                assertFalse(budget.cellAllowed())
                val previous = calls.get()
                try { budget.bind(raw,"cell").bind(-1); fail("LTE reopened") } catch (_: Exception) { }
                assertEquals(previous,calls.get())
                budget.bind(raw,"wifi").bind(-1)
                assertEquals(previous+1,calls.get())
                assertSame(budget,run.start(4096))
                run.stop()
                assertTrue(run.start(4096).cellAllowed())
            } finally { client.stop(); worker.shutdownNow() }
        }
    }
}
