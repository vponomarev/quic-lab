package ru.vpnc.quiclab

import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import androidx.test.platform.app.InstrumentationRegistry
import mobile.EventSink
import mobile.Gateway
import mobile.Mobile
import mobile.SocketBinder
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.CountDownLatch
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit

/** Real Android worker, native candidate engines and native router; no TUN or saved profiles. */
class MixedGroupVpnSessionTest {
    @Test fun invalidInitialQuicFallsBackToAwgBehindSameRootRouterAndRestartsSelectedCandidate() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        val cm = context.getSystemService(ConnectivityManager::class.java)
        assumeTrue("A physical Wi-Fi is required", cm.allNetworks.any {
            cm.getNetworkCapabilities(it)?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true
        })
        val key = android.util.Base64.encodeToString(ByteArray(32) { (it+1).toByte() },android.util.Base64.NO_WRAP)
        val awg = "[Interface]\nPrivateKey = $key\nAddress = 10.77.0.2\nDNS = 1.1.1.1\n" +
            "[Peer]\nPublicKey = $key\nAllowedIPs = 0.0.0.0/0\nEndpoint = 127.0.0.1:9\nPersistentKeepalive = 0\n"
        fun row(id: String, gateway: JSONObject) = JSONObject().put("connection_id",id)
            .put("transport",gateway.getString("transport")).put("gateway",gateway)
        val config = JSONObject().put("transport","quic").put("dns","10.77.0.1")
            .put(AdaptiveGroups.CANDIDATES,JSONArray()
                .put(row("bad-quic",JSONObject().put("transport","quic").put("endpoint","127.0.0.1:9")
                    .put("certificate","invalid synthetic cert").put("key","invalid synthetic key")))
                .put(row("awg",JSONObject().put("transport","awg").put("endpoint","127.0.0.1:9").put("awg_config",awg))))
        val directBinder = object : SocketBinder {
            override fun bind(fd: Long) { error("This fixture must never open a direct socket") }
        }
        val router = Mobile.createMultiRouterWithDNS(
            "[{\"id\":\"group-root\",\"mode\":\"subnets\",\"subnets\":[\"10.77.0.0/16\"]}]",
            "{\"mode\":\"tunnel\",\"exit_id\":\"group-root\"}",null,directBinder,
            object : EventSink { override fun onEvent(eventJSON: String) {} })
        val ready = CountDownLatch(1)
        val released = LinkedBlockingQueue<Gateway>()
        val gateways = CopyOnWriteArrayList<Gateway>()
        val events = LinkedBlockingQueue<JSONObject>()
        val failures = CopyOnWriteArrayList<JSONObject>()
        val session = VpnSession(cm,null,config,-1,
            availability = { _, kind, available -> if (kind == VpnSession.WIFI && available) ready.countDown() },
            attached = { gateway -> router.setGateway("group-root",gateway); gateways.add(gateway) },
            detached = { gateway -> router.detachGateway("group-root",gateway); released.offer(gateway) },
            output = { event ->
                events.offer(JSONObject(event.toString()))
                if (event.optString("reason") == "candidate_failed") failures.add(event)
            })
        fun awaitEvent(type: String, reason: String = ""): JSONObject {
            val deadline = android.os.SystemClock.elapsedRealtime()+10000
            while (android.os.SystemClock.elapsedRealtime() < deadline) {
                val event = events.poll(1,TimeUnit.SECONDS) ?: continue
                if (event.optString("event") == type && (reason.isBlank() || event.optString("reason") == reason)) return event
            }
            error("Missing $type/$reason; ${events.toList()}")
        }
        try {
            assertTrue("Wi-Fi callback missing",ready.await(10,TimeUnit.SECONDS))
            session.startOrMigrate(VpnSession.WIFI,"127.0.0.1:9","","",100)
            val first = awaitEvent("vless_group","started")
            assertEquals(2,first.getInt("index"));assertEquals("awg",first.getString("transport"))
            assertEquals("awg",gateways.single().localAddress())
            session.stop();awaitEvent("stopped")
            session.startOrMigrate(VpnSession.WIFI,"127.0.0.1:9","","",100)
            val restarted = awaitEvent("vless_group","started")
            assertEquals(2,restarted.getInt("index"));assertEquals("awg",restarted.getString("transport"))
            assertEquals(2,gateways.size);assertNotSame(gateways[0],gateways[1])
            assertEquals("Rejected QUIC identity should only be tried on the first start",1,failures.size)
            // The same immutable router still accepts the original root's DNS ownership.
            router.updateDNS("{\"mode\":\"tunnel\",\"exit_id\":\"group-root\"}",directBinder)
        } finally {
            session.close()
            try {
                repeat(gateways.size) { assertNotNull("candidate gateway was not detached",released.poll(10,TimeUnit.SECONDS)) }
            }
            finally { router.close() }
        }
    }
}
