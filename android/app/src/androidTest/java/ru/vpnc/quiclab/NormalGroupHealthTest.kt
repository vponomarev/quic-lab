package ru.vpnc.quiclab

import org.junit.Assert.*
import org.junit.Test

class NormalGroupHealthTest {
    private fun policy() = NormalGroupHealth(listOf("a", "b", "b", "c")).apply { reset(0, 0) }
    private fun slow(p: NormalGroupHealth) {
        p.active(0, 1000.0); p.active(30000, 1000.0); p.active(60000, 1000.0)
    }
    @Test fun sustainedDegradationNeedsRepeatedBetterCandidate() {
        val p = policy(); slow(p)
        p.candidate(1, 30000, 300.0)
        assertNull(p.replacement(60000))
        p.candidate(1, 60000, 300.0)
        assertEquals(1, p.replacement(60000))
    }
    @Test fun healthyPathDoesNotChaseFasterCandidate() {
        val p = policy()
        for (t in listOf(0L, 30000L, 60000L)) { p.active(t, 300.0); p.candidate(1, t, 50.0) }
        assertNull(p.replacement(60000))
    }
    @Test fun transientSpikeAndRecoveryBreakDegradationWindow() {
        val p = policy(); slow(p)
        p.candidate(1, 30000, 300.0); p.candidate(1, 60000, 300.0)
        p.active(60001, 200.0); p.active(90000, 1000.0)
        assertNull(p.replacement(90000))
    }
    @Test fun minimumDwellAndSampleSpanPreventOscillation() {
        val p = policy()
        for(t in listOf(0L, 15000L, 30000L)) { p.active(t, 1000.0); p.candidate(1,t,300.0) }
        assertNull(p.replacement(30000))
        p.reset(1, 60000)
        for(t in listOf(60000L, 60001L, 60002L)) { p.active(t,1000.0); p.candidate(0,t,300.0) }
        assertNull(p.replacement(60002))
    }
    @Test fun failedOrExpiredCandidateIsNotQualified() {
        val p = policy(); slow(p)
        p.candidate(1,30000,300.0); p.candidate(1,60000,300.0); p.candidate(1,60001,null)
        assertNull(p.replacement(60001))
        p.candidate(1,90000,300.0)
        assertNull(p.replacement(90000))
    }
    @Test fun staleActiveAndMissingSamplesCannotTriggerSwitch() {
        val p = policy(); slow(p)
        p.candidate(1,30000,300.0); p.candidate(1,60000,300.0)
        assertNull(p.replacement(110000))
        p.active(110000,1000.0)
        assertNull(p.replacement(110000))
        p.active(110001,null)
        assertNull(p.replacement(110001))
    }
    @Test fun invalidNumbersNeverQualify() {
        val p=policy(); slow(p)
        for(v in listOf(Double.NaN, Double.POSITIVE_INFINITY, -1.0, 0.0)) {
            p.candidate(1,30000,v); p.candidate(1,60000,v)
            assertNull(p.replacement(60000))
        }
    }
    @Test fun differenceMustBeMaterialInRelativeAndAbsoluteTerms() {
        val p=policy(); slow(p)
        p.candidate(1,30000,700.0); p.candidate(1,60000,700.0)
        assertNull(p.replacement(60000))
    }
    @Test fun resetDropsOldNetworkAndSessionEvidence() {
        val p=policy(); slow(p)
        p.candidate(1,30000,300.0); p.candidate(1,60000,300.0)
        p.reset(0,60000)
        assertNull(p.replacement(60000))
    }
    @Test fun backgroundQueueIsFairBetweenLogicalConnections() {
        val p=policy()
        assertEquals(listOf(1,3,2,3,1,3), (0..5).map { p.nextCandidate(it*30000L) })
    }
    @Test fun degradedPathConfirmsPromisingCandidateBeforeScanningMoreNodes() {
        val p=policy(); slow(p)
        val first=p.nextCandidate(60000)!!
        p.candidate(first,60000,300.0)
        assertEquals(first,p.nextCandidate(90000))
    }
    @Test fun loneCandidateHasNoBackgroundWork() {
        val p=NormalGroupHealth(listOf("a")); p.reset(0,0)
        assertNull(p.nextCandidate(60000)); assertNull(p.replacement(60000))
    }
}
