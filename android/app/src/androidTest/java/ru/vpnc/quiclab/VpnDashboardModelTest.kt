package ru.vpnc.quiclab

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class VpnDashboardModelTest {
    @Test fun decreasingCountersCannotInflateRunTotals() {
        val model = VpnDashboardModel()
        assertTrue(model.accept(snapshot(rxBytes = 100, txBytes = 50, at = 1000)))
        assertFalse(model.accept(snapshot(rxBytes = 90, txBytes = 40, at = 2000)))
        assertTrue(model.accept(snapshot(rxBytes = 110, txBytes = 60, at = 3000)))
        assertEquals(110L, model.exit("one")!!.rxTotalBytes)
        assertEquals(60L, model.exit("one")!!.txTotalBytes)
    }
    @Test
    fun staleGenerationAndOldExitIpResultAreIgnored() {
        val model = VpnDashboardModel(configuredIntervalMs = 1_000)
        assertTrue(model.accept(snapshot(generation = 2, exitIpv4 = "198.51.100.2", at = 10_000)))
        assertFalse(model.accept(snapshot(generation = 1, exitIpv4 = "198.51.100.1", at = 11_000)))

        val exit = model.exit("one")!!
        assertEquals("198.51.100.2", exit.exitIpv4)
        assertEquals(2L, exit.generation)
    }

    @Test
    fun disabledRttHasExplicitLabelAndStaleSamplesAreUnavailable() {
        val disabled = VpnDashboardModel(rttEnabled = false)
        assertTrue(disabled.accept(snapshot(path = path(rttMs = 12.0, measuredAtMs = 10_000), at = 10_000)))
        assertEquals("RTT выключен", disabled.exit("one")!!.rttLabel)
        assertNull(disabled.exit("one")!!.rttMs)

        val stale = VpnDashboardModel(configuredIntervalMs = 1_000)
        assertTrue(stale.accept(snapshot(path = path(rttMs = 12.0, measuredAtMs = 10_000), at = 10_000)))
        assertEquals("Недоступно", stale.exit("one")!!.rttLabelAt(15_001))
        assertNull(stale.exit("one")!!.rttMsAt(15_001))
    }

    @Test
    fun reconnectKeepsPerExitTotalsAndNewRunResetsThem() {
        val model = VpnDashboardModel()
        model.beginRun("run")
        assertTrue(model.accept(snapshot(generation = 1, rxBytes = 100, txBytes = 20, at = 1_000)))
        assertTrue(model.accept(snapshot(generation = 2, rxBytes = 40, txBytes = 8, at = 2_000)))
        val beforeNewRun = model.exit("one")!!
        assertEquals(140L, beforeNewRun.rxTotalBytes)
        assertEquals(28L, beforeNewRun.txTotalBytes)

        model.beginRun("new")
        assertTrue(model.accept(snapshot(runId = "new", generation = 1, rxBytes = 3, txBytes = 4, at = 3_000)))
        val newRun = model.exit("one")!!
        assertEquals(3L, newRun.rxTotalBytes)
        assertEquals(4L, newRun.txTotalBytes)
    }

    @Test
    fun missingRadioIsUnavailableAndExitTotalsStaySeparate() {
        val model = VpnDashboardModel()
        assertTrue(model.accept(snapshot(exitId = "wifi", rxBytes = 10, txBytes = 2, at = 1_000)))
        assertTrue(model.accept(snapshot(exitId = "lte", rxBytes = 30, txBytes = 4, at = 1_000)))

        assertEquals(10L, model.exit("wifi")!!.rxTotalBytes)
        assertEquals(30L, model.exit("lte")!!.rxTotalBytes)
        assertEquals("Недоступно", model.exit("wifi")!!.radioLabel)
        assertNull(model.exit("wifi")!!.radio)
    }

    @Test
    fun foreignRunIsIgnoredUntilExplicitRunStart() {
        val model = VpnDashboardModel()
        assertTrue(model.accept(snapshot(runId = "run-a", rxBytes = 10, at = 1_000)))
        assertFalse(model.accept(snapshot(runId = "run-b", rxBytes = 90, at = 2_000)))
        assertEquals(10L, model.exit("one")!!.rxTotalBytes)

        model.beginRun("run-b")
        assertTrue(model.accept(snapshot(runId = "run-b", rxBytes = 3, at = 3_000)))
        assertEquals(3L, model.exit("one")!!.rxTotalBytes)
    }

    @Test
    fun olderSampleInSameGenerationCannotRollbackState() {
        val model = VpnDashboardModel()
        assertTrue(model.accept(snapshot(exitIpv4 = "198.51.100.2", rxBytes = 10, at = 2_000)))
        assertFalse(model.accept(snapshot(exitIpv4 = "198.51.100.1", rxBytes = 7, at = 1_000)))
        assertEquals("198.51.100.2", model.exit("one")!!.exitIpv4)
        assertEquals(10L, model.exit("one")!!.rxTotalBytes)
    }

    @Test
    fun invalidCountersAndRatesAreRejected() {
        val model = VpnDashboardModel()
        assertTrue(model.accept(snapshot(rxBytes = 10, txBytes = 5, at = 1_000)))
        assertFalse(model.accept(snapshot(generation = 2, rxBytes = -1, txBytes = 2, at = 2_000)))
        assertFalse(model.accept(snapshot(generation = 2, rxBytes = 2, txBytes = 2, rxBps = -1.0, at = 2_000)))
        assertEquals(10L, model.exit("one")!!.rxTotalBytes)
    }

    @Test
    fun staleOrDisabledJitterIsUnavailableAndRadioPermissionHidesSignal() {
        val sample = path(rttMs = 12.0, measuredAtMs = 1_000).copy(jitterMs = 4.0)
        val stale = VpnDashboardModel(configuredIntervalMs = 1_000)
        assertTrue(stale.accept(snapshot(path = sample, at = 7_001)))
        assertNull(stale.exit("one")!!.jitterMs)

        val disabled = VpnDashboardModel(rttEnabled = false)
        assertTrue(disabled.accept(snapshot(path = sample, at = 1_000)))
        assertNull(disabled.exit("one")!!.jitterMs)

        val denied = VpnDashboardModel(radioPermissionGranted = false)
        assertTrue(denied.accept(snapshot(radio = RadioSnapshot("LTE", -70, 1_000), at = 1_000)))
        assertNull(denied.exit("one")!!.radio)
        assertEquals("Недоступно", denied.exit("one")!!.radioLabel)
    }

    private fun snapshot(
        runId: String = "run",
        exitId: String = "one",
        generation: Long = 1,
        rxBytes: Long = 0,
        txBytes: Long = 0,
        rxBps: Double = 0.0,
        radio: RadioSnapshot? = null,
        exitIpv4: String? = null,
        path: PathSnapshot? = null,
        at: Long = 1_000,
    ) = ExitSnapshot(
        exitId = exitId,
        generation = generation,
        state = "connected",
        activePathId = path?.pathId,
        paths = listOfNotNull(path),
        rxBytes = rxBytes,
        txBytes = txBytes,
        rxBps = rxBps,
        txBps = 0.0,
        exitIpv4 = exitIpv4,
        runId = runId,
        capturedAtMs = at,
        radio = radio,
    )

    private fun path(
        rttMs: Double?,
        measuredAtMs: Long,
    ) = PathSnapshot("path", "profile", "wifi", "active", rttMs, null, measuredAtMs)
}
