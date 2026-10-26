package ru.vpnc.quiclab

import org.junit.Assert.*
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

class MixedGroupSessionTest {
    private class Client(val transport: String) { var closed = false }
    private class Router {
        var active: Client? = null
        val order = mutableListOf<String>()
        var reject = false
        val session = MixedGroupSession<Client>(VlessGroupPolicy(3, listOf("subscription", "subscription", "awg")),
            attach = { if (reject) error("attach failed"); active = it; order.add("attach:${it.transport}") },
            detach = { if (active === it) active = null; order.add("detach:${it.transport}") },
            dispose = { it.closed = true; order.add("close:${it.transport}") })
    }

    @Test fun failedInitialCandidateAdvancesToAnotherLogicalConnection() {
        val router = Router()
        assertFalse(router.session.replace(0, 0) { error("dial failed") })
        assertEquals(2, router.session.policy.retry(1))
        assertTrue(router.session.replace(2, 1) { Client("awg") })
        assertEquals("awg", router.active?.transport)
    }

    @Test fun engineStartedWithoutUsefulHealthEventuallyAdvances() {
        val router = Router()
        assertTrue(router.session.replace(0, 0) { Client("awg") })
        assertNull(router.session.check(14999, true))
        assertNull(router.session.check(15000, true))
        assertEquals(2, router.session.check(20000, true))
    }

    @Test fun healthLossClosesOldClientBeforeAttachingReplacement() {
        val router = Router()
        val awg = Client("awg")
        router.session.replace(0, 0) { awg }
        router.session.healthy(router.session.generation, 10000)
        assertNull(router.session.check(24999, true))
        assertNull(router.session.check(25000, true))
        assertEquals(2, router.session.check(30000, true))
        router.session.replace(2, 30000) { Client("https") }
        assertTrue(awg.closed)
        assertEquals(listOf("attach:awg", "detach:awg", "close:awg", "attach:https"), router.order)
        assertEquals("https", router.active?.transport)
    }

    @Test fun staleHealthAndFailureCannotAffectReplacement() {
        val router = Router()
        router.session.replace(0, 0) { Client("vless") }
        val old = router.session.generation
        router.session.replace(2, 1) { Client("awg") }
        assertFalse(router.session.accepts(old))
        assertNull(router.session.failed(old, 16000))
        router.session.healthy(old, 19000)
        assertNull(router.session.check(15001, true))
        assertEquals(0, router.session.check(20001, true))
        assertEquals(2, router.session.policy.current)
    }

    @Test fun closeDuringAcquisitionDisposesLateClientWithoutAttaching() {
        val router = Router()
        val late = Client("quic")
        val acquiring = CountDownLatch(1)
        val release = CountDownLatch(1)
        var attached = true
        val dial = Thread {
            attached = router.session.replace(0, 0) {
                acquiring.countDown()
                check(release.await(5, TimeUnit.SECONDS))
                late
            }
        }
        dial.start()
        // Stub RED returns before acquire; release the worker before asserting.
        val entered = acquiring.await(1, TimeUnit.SECONDS)
        router.session.close()
        release.countDown()
        dial.join(5000)
        assertTrue("candidate acquisition was never entered", entered)
        assertFalse(attached)
        assertNull(router.active)
        assertTrue(late.closed)
        assertEquals(listOf("close:quic"), router.order)
    }

    @Test fun attachmentFailureDisposesCandidateAndAllowsRetry() {
        val router = Router()
        val failed = Client("vless")
        router.reject = true
        assertFalse(router.session.replace(0, 0) { failed })
        assertTrue(failed.closed)
        assertNull(router.active)
        router.reject = false
        assertTrue(router.session.replace(2, 1) { Client("awg") })
        assertEquals("awg", router.active?.transport)
    }

    @Test fun rejectedAttachmentInvalidatesQueuedNativeEvents() {
        val router = Router()
        var failedGeneration = -1L
        router.reject = true
        assertFalse(router.session.replace(0, 0) { token -> failedGeneration = token; Client("quic") })
        assertFalse("failed client can still publish connected/health events", router.session.accepts(failedGeneration))
    }

    @Test fun stoppedSessionRestartsFromSelectedCandidateAndCloseIsFinal() {
        val router = Router()
        router.session.replace(2, 0) { Client("https") }
        router.session.stop()
        assertEquals(2, router.session.policy.current)
        assertTrue(router.session.replace(router.session.policy.current, 10) { Client("https") })
        router.session.close()
        assertFalse(router.session.replace(0, 20) { fail("closed session acquired a client"); Client("quic") })
        assertNull(router.active)
    }
}
