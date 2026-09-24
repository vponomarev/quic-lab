package ru.vpnc.quiclab

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.net.Network
import android.net.NetworkRequest
import android.os.ParcelFileDescriptor
import androidx.test.ext.junit.runners.AndroidJUnit4
import androidx.test.platform.app.InstrumentationRegistry
import mobile.EventSink
import mobile.Mobile
import mobile.SocketBinder
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import org.junit.runner.RunWith
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit
import java.net.DatagramSocket

/** On-device integration test. Requires reachable lab server and an active Wi-Fi network. */
@RunWith(AndroidJUnit4::class)
class WifiMigrationTest {
    @Test fun boundSocketKeepsConnectionAcrossTwoMigrations() {
        val instrumentation = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        val endpoint = requireNotNull(args.getString("server")) { "Pass -e server IP:port" }
        val pin = args.getString("pin") ?: ""
        val cm = instrumentation.targetContext.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        val available = LinkedBlockingQueue<Network>()
        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) { available.offer(network) }
        }
        cm.requestNetwork(NetworkRequest.Builder()
            .addTransportType(NetworkCapabilities.TRANSPORT_WIFI)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET).build(), callback)
        val network = available.poll(10, TimeUnit.SECONDS) ?: run {
            cm.unregisterNetworkCallback(callback)
            error("Connect the phone to Wi-Fi before running the lab test")
        }
        fun binderFor(selected: Network) = object : SocketBinder {
            override fun bind(fd: Long) {
                ParcelFileDescriptor.fromFd(fd.toInt()).use { selected.bindSocket(it.fileDescriptor) }
            }
        }
        var cellularCallback: ConnectivityManager.NetworkCallback? = null
        val events = LinkedBlockingQueue<JSONObject>()
        val client = Mobile.newClient(object : EventSink {
            override fun onEvent(eventJSON: String) { events.offer(JSONObject(eventJSON)) }
        })
        fun echoAfter(previousPeer: String?): JSONObject {
            val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(12)
            while (System.nanoTime() < deadline) {
                val event = events.poll(1, TimeUnit.SECONDS) ?: continue
                if (event.optString("event") == "echo" && event.optString("peer") != previousPeer) return event
            }
            error("No echo on the expected path")
        }
        try {
            // Isolate Android network permission failures from the Go fd bridge.
            DatagramSocket().use { network.bindSocket(it) }
            val resolved = resolveEndpoint(endpoint, network)
            client.start(resolved.address, args.getString("name") ?: resolved.hostname, pin, 50L, binderFor(network))
            val first = echoAfter(null)
            var previous = first
            val alternate = if (args.getString("migrationNetwork") == "cellular") {
                val ready = LinkedBlockingQueue<Network>()
                val cb = object : ConnectivityManager.NetworkCallback() {
                    override fun onAvailable(network: Network) { ready.offer(network) }
                }
                cellularCallback = cb
                cm.requestNetwork(NetworkRequest.Builder().addTransportType(NetworkCapabilities.TRANSPORT_CELLULAR)
                    .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET).build(), cb)
                ready.poll(20, TimeUnit.SECONDS) ?: error("Cellular network unavailable")
            } else network
            for (target in listOf(alternate, network)) {
                client.migrate(binderFor(target))
                val next = echoAfter(previous.getString("peer"))
                assertEquals(first.getString("connection_id"), next.getString("connection_id"))
                assertEquals(first.getLong("stream_id"), next.getLong("stream_id"))
                assertTrue(next.getLong("seq") > previous.getLong("seq"))
                android.util.Log.i("QuicLabTest", "same connection=${next.getString("connection_id")} stream=${next.getLong("stream_id")} peer=${next.getString("peer")}")
                previous = next
            }
        } finally {
            client.stop()
            cellularCallback?.let { cm.unregisterNetworkCallback(it) }
            cm.unregisterNetworkCallback(callback)
        }
    }
}
