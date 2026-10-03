package ru.vpnc.quiclab

import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class VpnDashboardEventsTest {
    @Test fun vlessHealthWithoutRttAndExpiredProof() {
        val bridge = VpnDashboardEventStore()
        bridge.beginRun("run", mapOf("exit" to "Exit"))
        bridge.record("run", "exit", 1, JSONObject("""{"event":"connected","transport":"vless"}"""), 1000)
        bridge.record("run", "exit", 1, JSONObject("""{"event":"health"}"""), 2000)
        assertTrue(bridge.dashboards(2000).single().state.contains("подтверждена"))
        assertTrue(bridge.dashboards(33000).single().state.contains("нет свежего подтверждения"))
        bridge.record("run", "exit", 2, JSONObject("""{"event":"connected","transport":"vless"}"""), 34000)
        bridge.record("run", "exit", 1, JSONObject("""{"event":"health"}"""), 35000)
        assertFalse(bridge.dashboards(35000).single().state.contains("подтверждена"))
        bridge.stopExit("exit", 3)
        assertFalse(bridge.dashboards(36000).single().state.contains("TCP до сервера"))
    }
    @Test fun vlessTcpDoesNotClaimWorkingTunnel() {
        val bridge = VpnDashboardEventStore()
        bridge.beginRun("run", mapOf("exit" to "Exit"))
        fun event(raw: String, at: Long) { bridge.record("run", "exit", 1, JSONObject(raw), at) }
        event("""{"event":"connected","transport":"vless"}""", 1000)
        assertTrue(bridge.dashboards(1000).single().state.contains("Передача через VPN: проверяется"))
        event("""{"event":"vless_tcp","tcp_state":"established"}""", 2000)
        assertTrue(bridge.dashboards(2000).single().state.contains("TCP до сервера: установлен"))
        assertFalse(bridge.dashboards(2000).single().state.contains("подтверждена"))
        event("""{"event":"echo","rtt_ms":20}""", 3000)
        assertTrue(bridge.dashboards(3000).single().state.contains("Передача через VPN: подтверждена"))
        event("""{"event":"probe_unavailable"}""", 4000)
        assertTrue(bridge.dashboards(4000).single().state.contains("проверка не прошла"))
        event("""{"event":"reconnecting"}""", 5000)
        assertFalse(bridge.dashboards(5000).single().state.contains("подтверждена"))
    }
    @Test fun physicalNetworkChangeCannotReusePreviousLatency() {
        val bridge = VpnDashboardEventStore()
        bridge.beginRun("run", mapOf("exit" to "Exit"))
        bridge.record("run", "exit", 1, JSONObject("""{"event":"active_network","detail":"Wi-Fi"}"""), 1000)
        bridge.record("run", "exit", 1, JSONObject("""{"event":"echo","rtt_ms":10}"""), 1000)
        bridge.record("run", "exit", 1, JSONObject("""{"event":"echo","rtt_ms":14}"""), 1500)
        bridge.record("run", "exit", 1, JSONObject("""{"event":"active_network","detail":"LTE/Cellular"}"""), 2000)
        val card = bridge.dashboards(2000).single()
        val path = card.paths.first { it.pathId == card.activePathId }
        assertNull("New physical network must wait for its own RTT", path.rttMs)
        assertNull("Jitter must not join samples from different physical networks", path.jitterMs)
        assertEquals("LTE/Cellular", path.network)
    }
    @Test fun candidateFailureDoesNotChangeHealthyExitStatus() {
        val bridge = VpnDashboardEventStore()
        bridge.beginRun("run", mapOf("exit" to "Exit"))
        bridge.record("run", "exit", 1, JSONObject("""{"event":"connected","path_id":"working","network":"wifi"}"""), 1000)
        bridge.record("run", "exit", 1, JSONObject("""{"event":"operation_failed","path_id":"candidate","network":"cell"}"""), 2000)
        val card = bridge.dashboards(2000).single()
        assertEquals("Подключён", card.state)
        assertEquals("working", card.activePathId)
    }
    @Test fun runCountersSurviveReconnectAndLateEventsCannotReplaceIp() {
        val bridge = VpnDashboardEventStore()
        bridge.beginRun("run", mapOf("internet" to "Internet", "home" to "Home"))
        bridge.record("run", "internet", 1, JSONObject("""{"event":"profile_traffic","rx_bytes":100,"tx_bytes":50}"""), 1000)
        bridge.record("run", "internet", 2, JSONObject("""{"event":"connected","connection_id":"new","path_id":"q/wifi","profile_id":"q","network":"wifi"}"""), 2000)
        bridge.record("run", "internet", 2, JSONObject("""{"event":"profile_traffic","rx_bytes":130,"tx_bytes":65}"""), 3000)
        bridge.record("run", "internet", 1, JSONObject("""{"event":"exit_ip","ip":"192.0.2.1"}"""), 3100)
        bridge.record("old-run", "internet", 3, JSONObject("""{"event":"exit_ip","ip":"192.0.2.2"}"""), 3200)
        bridge.record("run", "home", 1, JSONObject("""{"event":"profile_traffic","rx_bytes":7,"tx_bytes":3}"""), 3000)
        val cards = bridge.dashboards(3300).associateBy { it.exitId }
        assertEquals(130L, cards.getValue("internet").rxTotalBytes)
        assertEquals(65L, cards.getValue("internet").txTotalBytes)
        assertEquals(7L, cards.getValue("home").rxTotalBytes)
        assertNull(cards.getValue("internet").exitIpv4)
        assertEquals(15.0, cards.getValue("internet").rxBps, 0.01)
    }

    @Test fun alternateEchoDoesNotSelectPathOrFreshenFromRepeatedStats() {
        val bridge = VpnDashboardEventStore()
        bridge.beginRun("run", mapOf("exit" to "Exit"))
        bridge.record("run", "exit", 1, JSONObject("""{"event":"connected","path_id":"wifi","network":"wifi"}"""), 1000)
        bridge.record("run", "exit", 1, JSONObject("""{"event":"echo","path_id":"wifi","rtt_ms":10}"""), 1000)
        bridge.record("run", "exit", 1, JSONObject("""{"event":"echo","path_id":"wifi","rtt_ms":14}"""), 2000)
        bridge.record("run", "exit", 1, JSONObject("""{"event":"echo","path_id":"cell","network":"cell","rtt_ms":40}"""), 2000)
        bridge.record("run", "exit", 1, JSONObject("""{"event":"bond_stats","paths":[{"name":"wifi","ready":true,"rtt_ms":14},{"name":"cell","ready":true,"rtt_ms":40}]}"""), 9000)
        val card = bridge.dashboards(9000).single()
        assertEquals("wifi", card.activePathId)
        assertTrue(VpnDashboardText.card(card, "Exit", 9000, 1000, "Недоступно").contains("RTT: Недоступно"))
        assertTrue(VpnDashboardText.card(card, "Exit", 2100, 0, "Недоступно").contains("RTT выключен"))
        assertTrue(VpnDashboardText.card(card, "Exit", 2100, 1000, "Недоступно").contains("Jitter max · 15 с: 4.0 мс"))
        assertTrue(VpnDashboardText.card(card, "Exit", 2100, 1000, "Недоступно").contains("Exit IPv4: Недоступен"))
    }
}
