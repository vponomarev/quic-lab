package ru.vpnc.quiclab

import org.junit.Assert.*
import org.junit.Test
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicInteger

class NormalGroupProberTest {
    private class Probe(private val work: () -> Double = { 100.0 }) : GroupPathMeasurement {
        val disposed = CountDownLatch(1)
        val canceled = CountDownLatch(1)
        override fun measure() = work()
        override fun cancel() { canceled.countDown() }
        override fun close() { disposed.countDown() }
    }
    @Test fun oneWorkerMeasuresActiveThenCandidateAndReportsBoth() {
        val delivered=CountDownLatch(1); val calls=mutableListOf<String>()
        var result: Pair<Double?,Double?>?=null
        val p=NormalGroupProber { it() }
        try {
            assertTrue(p.start({true}, { Probe { calls.add("active");1000.0 } }, { Probe { calls.add("candidate");200.0 } }) { a,b -> result=a to b; delivered.countDown() })
            assertTrue(delivered.await(3,TimeUnit.SECONDS))
            assertEquals(listOf("active","candidate"),calls)
            assertEquals(1000.0 to 200.0,result)
        } finally { p.close() }
    }
    @Test fun cancelInterruptsMeasurementButDoesNotOverlapAnotherJob() {
        val entered=CountDownLatch(1);val release=CountDownLatch(1)
        val active=Probe { entered.countDown();check(release.await(3,TimeUnit.SECONDS));100.0 }
        val callbacks=AtomicInteger();val candidateCalls=AtomicInteger()
        val p=NormalGroupProber { it() }
        try {
            assertTrue(p.start({true},{active},{ candidateCalls.incrementAndGet();Probe() }) { _,_-> callbacks.incrementAndGet() })
            assertTrue(entered.await(3,TimeUnit.SECONDS));p.cancel()
            assertTrue(active.canceled.await(1,TimeUnit.SECONDS))
            assertFalse(p.start({true},{Probe()},{Probe()}) { _,_-> })
            release.countDown()
            assertTrue(active.disposed.await(3,TimeUnit.SECONDS))
            p.close();assertTrue(p.awaitStopped())
            assertEquals(0,candidateCalls.get());assertEquals(0,callbacks.get())
        } finally {release.countDown();p.close()}
    }
    @Test fun lateAcquisitionIsDisposedAndNeverMeasured() {
        val entered=CountDownLatch(1);val release=CountDownLatch(1)
        val measured=AtomicInteger();val late=Probe { measured.incrementAndGet();100.0 }
        val p=NormalGroupProber { it() }
        try {
            p.start({true},{entered.countDown();check(release.await(3,TimeUnit.SECONDS));late},{Probe()}) { _,_-> fail("late callback") }
            assertTrue(entered.await(3,TimeUnit.SECONDS));p.close();release.countDown()
            assertTrue(p.awaitStopped());assertTrue(late.disposed.await(1,TimeUnit.SECONDS));assertEquals(0,measured.get())
        } finally {release.countDown();p.close()}
    }
    @Test fun permissionRevocationSkipsCandidateAndResult() {
        val allowed=java.util.concurrent.atomic.AtomicBoolean(true);val candidates=AtomicInteger();val callbacks=AtomicInteger()
        val executor=java.util.concurrent.Executors.newSingleThreadExecutor()
        val p=NormalGroupProber(executor) { it() }
        try {
            p.start({allowed.get()},{Probe {allowed.set(false);100.0}},{candidates.incrementAndGet();Probe()}) { _,_->callbacks.incrementAndGet() }
            executor.shutdown()
            assertTrue(executor.awaitTermination(3,TimeUnit.SECONDS));assertEquals(0,candidates.get());assertEquals(0,callbacks.get())
        } finally { p.close() }
    }
    @Test fun queuedResultIsRejectedAfterCancellation() {
        val posted=CountDownLatch(1);var callback:(()->Unit)?=null;val calls=AtomicInteger()
        val p=NormalGroupProber { callback=it;posted.countDown() }
        try {
            p.start({true},{Probe()},{Probe()}) { _,_->calls.incrementAndGet() }
            assertTrue(posted.await(3,TimeUnit.SECONDS));p.cancel();callback!!.invoke()
            assertEquals(0,calls.get())
        } finally {p.close()}
    }

    @Test fun acquisitionGateClosesDuringBlockedDnsBeforeTransportStartup() {
        val entered=CountDownLatch(1);val release=CountDownLatch(1);val starts=AtomicInteger()
        val executor=java.util.concurrent.Executors.newSingleThreadExecutor()
        val p=NormalGroupProber(executor) { it() }
        try {
            p.start({true},{Probe()},{valid ->
                entered.countDown();check(release.await(3,TimeUnit.SECONDS))
                if(valid()) starts.incrementAndGet()
                Probe()
            }) { _,_-> }
            assertTrue(entered.await(3,TimeUnit.SECONDS));p.cancel();release.countDown()
            executor.shutdown();assertTrue(executor.awaitTermination(3,TimeUnit.SECONDS))
            assertEquals("canceled DNS completion started a transport",0,starts.get())
        } finally {release.countDown();p.close()}
    }

    @Test fun canceledCandidateKeepsForegroundAcquisitionBlockedUntilPeerReleased() {
        val measuring=CountDownLatch(1);val disposeEntered=CountDownLatch(1);val release=CountDownLatch(1)
        val peerOwned=java.util.concurrent.atomic.AtomicBoolean(false)
        val p=NormalGroupProber { it() }
        try {
            p.start({true},{Probe()},{
                peerOwned.set(true)
                object : GroupPathMeasurement {
                    val canceled=CountDownLatch(1)
                    override fun measure():Double { measuring.countDown();check(canceled.await(3,TimeUnit.SECONDS));return 100.0 }
                    override fun cancel(){canceled.countDown()}
                    override fun close(){disposeEntered.countDown();check(release.await(3,TimeUnit.SECONDS));peerOwned.set(false)}
                }
            }) { _,_-> }
            assertTrue(measuring.await(3,TimeUnit.SECONDS));p.cancel()
            assertTrue(disposeEntered.await(3,TimeUnit.SECONDS))
            assertTrue("foreground may acquire an AWG peer while temporary disposal still owns it",p.isBusy())
            assertTrue(peerOwned.get())
            release.countDown();p.close();assertTrue(p.awaitStopped())
            assertFalse(p.isBusy());assertFalse(peerOwned.get())
        } finally {release.countDown();p.close()}
    }
}
