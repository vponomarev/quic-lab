package ru.vpnc.quiclab

import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class TransportStatsTest {
    @Test fun transitKeepsLastSuccessAcrossMissingProbes() {
        val stats=TransportStats()
        stats.accept(JSONObject("{event:transit_echo,rtt_ms:123}"),100)
        stats.accept(JSONObject("{event:transit_probe_failed}"),2000)
        assertEquals(123.0,stats.transitRtt,0.001)
        assertEquals(100L,stats.lastTransitEcho)
        stats.accept(JSONObject("{event:transit_echo,rtt_ms:99}"),5000)
        assertEquals(99.0,stats.transitRtt,0.001)
        assertEquals(5000L,stats.lastTransitEcho)
    }

    @Test fun missingGapDoesNotPoisonHttpsMetrics() {
        val stats=TransportStats()
        stats.accept(JSONObject("{event:echo,rtt_ms:20}"),100)
        stats.accept(JSONObject("{event:echo,rtt_ms:25}"),250)
        assertEquals(150.0,stats.maxGap,0.01)
    }

    private fun echo(stats: TransportStats, time: Long, rtt: Double) {
        stats.accept(JSONObject().put("event", "echo").put("rtt_ms", rtt).put("gap_ms", 50), time)
    }
    @Test fun jitterExpiresUsesBothSamplesAndFreezesOnStop() {
        val stats = TransportStats()
        assertNull(stats.maxJitter(100))
        echo(stats, 100, 20.0)
        assertNull(stats.maxJitter(100))
        echo(stats, 200, 120.0)
        echo(stats, 300, 110.0)
        assertEquals(100.0, stats.maxJitter(300)!!, 0.001)
        assertEquals(10.0, stats.maxJitter(15101)!!, 0.001)
        assertNull(stats.maxJitter(15201))
        echo(stats, 16000, 20.0)
        echo(stats, 16050, 23.5)
        stats.accept(JSONObject("{event:stopped}"), 16100)
        assertEquals(3.5, stats.maxJitter(90000)!!, 0.001)
    }
    @Test fun jitterIncludesRttStepAcrossReconnectButNotOutageDuration() {
        val stats = TransportStats()
        echo(stats, 100, 20.0)
        stats.accept(JSONObject("{event:disconnected}"), 200)
        stats.accept(JSONObject("{event:connected}"), 2000)
        echo(stats, 2100, 70.0)
        assertEquals(50.0, stats.maxJitter(2100)!!, 0.001)
        assertEquals(2000.0, stats.maxGap, 0.001)
        assertNull(stats.maxJitter(18000))
    }
    @Test fun gapSpansReconnectAndIdentitiesComeFromEcho() {
        val stats = TransportStats()
        stats.accept(JSONObject("{event:connected}"), 100)
        stats.accept(JSONObject("{event:echo,connection_id:one,rtt_ms:20,gap_ms:50}"), 200)
        stats.accept(JSONObject("{event:disconnected}"), 300)
        stats.accept(JSONObject("{event:connected}"), 700)
        stats.accept(JSONObject("{event:echo,connection_id:two,rtt_ms:25,gap_ms:50}"), 850)
        assertEquals(650.0, stats.maxGap, 0.01)
        assertEquals(25.0, stats.rtt, 0.01)
        assertEquals(2, stats.sessions.size)
    }
}
