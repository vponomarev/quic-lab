package ru.vpnc.quiclab

import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class VpnEchoTest {
    private class Backend : EchoDiagnosticBackend {
        var active = true
        var available = setOf("home", "internet")
        val starts = mutableListOf<Triple<String, String, Long>>()
        val callbacks = mutableListOf<(JSONObject) -> Unit>()
        var closes = 0
        override fun vpnActive() = active
        override fun exits() = available
        override fun start(exitId: String, target: String, intervalMs: Long, output: (JSONObject) -> Unit): AutoCloseable {
            starts.add(Triple(exitId,target,intervalMs)); callbacks.add(output)
            return AutoCloseable { closes++ }
        }
    }
    @Test fun runningVpnAndSelectedExitEchoAllowedButStandaloneDenied() {
        val backend = Backend(); val controller = EchoDiagnosticController(backend)
        assertFalse(controller.canOpenStandalone())
        assertTrue(controller.start("home","responder.test:9000",1000))
        assertEquals(listOf(Triple("home","responder.test:9000",1000L)),backend.starts)
        controller.close(); assertEquals(1,backend.closes)
    }
    @Test fun unavailableExitNeverStartsAnotherExitOrDirectFallback() {
        val backend = Backend(); val controller = EchoDiagnosticController(backend)
        assertFalse(controller.start("missing","responder.test:9000",1000))
        backend.active = false
        assertFalse(controller.start("home","responder.test:9000",1000))
        assertTrue(controller.canOpenStandalone()); assertTrue(backend.starts.isEmpty())
    }
    @Test fun targetFailureIsDiagnosticAndDoesNotPromoteAnotherExit() {
        val backend = Backend(); val controller = EchoDiagnosticController(backend)
        assertTrue(controller.start("home","responder.test:9000",1000))
        backend.callbacks.single()(JSONObject().put("event","exit_echo").put("error","responder unavailable"))
        assertEquals("responder unavailable",controller.result?.optString("error"))
        assertTrue(controller.running); assertEquals(1,backend.starts.size)
        assertEquals("home",backend.starts.single().first)
        controller.close()
    }
    @Test fun closingOrReplacingDiagnosticRejectsLateCallbackAndStopsOnlyOwnedHandle() {
        val backend = Backend(); val controller = EchoDiagnosticController(backend)
        assertTrue(controller.start("home","first.test:9000",1000))
        val old = backend.callbacks.single()
        assertTrue(controller.start("internet","second.test:9000",1000))
        assertEquals(1,backend.closes)
        old(JSONObject().put("event","exit_echo").put("rtt_ms",999))
        assertNull(controller.result)
        backend.callbacks.last()(JSONObject().put("event","exit_echo").put("rtt_ms",12))
        assertEquals(12.0,controller.result!!.getDouble("rtt_ms"),0.0)
        controller.close(); controller.close(); assertEquals(2,backend.closes)
        backend.callbacks.last()(JSONObject().put("event","exit_echo").put("rtt_ms",999))
        assertNull(controller.result); assertFalse(controller.running)
    }
    @Test fun invalidTargetOrUnboundedIntervalDoesNotReachGateway() {
        val backend = Backend(); val controller = EchoDiagnosticController(backend)
        for (target in listOf("","https://responder.test","host:0","host:65536","host:abc")) {
            assertFalse(controller.start("home",target,1000))
        }
        for (interval in listOf(0L,99L,60001L)) assertFalse(controller.start("home","host:9000",interval))
        assertTrue(backend.starts.isEmpty())
    }
    @Test fun asynchronousStartFailureReleasesLeaseAndAllowsRetry() {
        val backend = Backend(); val controller = EchoDiagnosticController(backend)
        assertTrue(controller.start("home","responder.test:9000",1000))
        backend.callbacks.single()(JSONObject().put("event","exit_echo").put("terminal",true)
            .put("error","Выбранный VPN-выход недоступен"))
        assertFalse(controller.running); assertNull(controller.result); assertEquals(1,backend.closes)
        assertEquals("Выбранный VPN-выход недоступен",controller.message)
        assertTrue(controller.start("home","responder.test:9000",1000)); controller.close()
    }
    @Test fun stoppedOrRestartedExitClearsLastRttAndRejectsLaterResults() {
        for (reason in listOf("VPN-выход остановлен","VPN-выход переподключён")) {
            val backend = Backend(); val controller = EchoDiagnosticController(backend)
            assertTrue(controller.start("home","responder.test:9000",1000))
            val callback = backend.callbacks.single()
            callback(JSONObject().put("event","exit_echo").put("rtt_ms",12))
            assertNotNull(controller.result)
            callback(JSONObject().put("event","exit_echo").put("terminal",true).put("error",reason))
            assertFalse(controller.running); assertNull(controller.result); assertEquals(1,backend.closes)
            assertEquals(reason,controller.message)
            callback(JSONObject().put("event","exit_echo").put("rtt_ms",999))
            assertNull(controller.result)
        }
    }
}
