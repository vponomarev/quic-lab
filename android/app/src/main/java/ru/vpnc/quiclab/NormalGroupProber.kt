package ru.vpnc.quiclab

internal interface GroupPathMeasurement : AutoCloseable {
    fun measure(): Double
    fun cancel()
}

internal class NormalGroupProber(private val worker: java.util.concurrent.ExecutorService = java.util.concurrent.Executors.newSingleThreadExecutor(), private val post: (() -> Unit) -> Unit) : AutoCloseable {
    private val lock = Any()
    private var generation = 0L
    private var busy = false
    private var closed = false
    private var measuring: GroupPathMeasurement? = null

    fun start(allowed: () -> Boolean, active: (() -> Boolean) -> GroupPathMeasurement, candidate: (() -> Boolean) -> GroupPathMeasurement,
              result: (Double?, Double?) -> Unit): Boolean {
        val token = synchronized(lock) {
            if (closed || busy || !allowed()) return false
            busy = true
            ++generation
        }
        try {
            worker.execute {
                try {
                    val a = measure(token, allowed, active)
                    if (!accepts(token) || !allowed()) return@execute
                    val b = measure(token, allowed, candidate)
                    if (!accepts(token) || !allowed()) return@execute
                    post { if (accepts(token) && allowed()) result(a, b) }
                } finally { synchronized(lock) { busy = false } }
            }
        } catch (_: java.util.concurrent.RejectedExecutionException) {
            synchronized(lock) { busy = false }
            return false
        }
        return true
    }

    private fun accepts(token: Long) = synchronized(lock) { !closed && token == generation }

    private fun measure(token: Long, allowed: () -> Boolean, acquire: (() -> Boolean) -> GroupPathMeasurement): Double? {
        if (!accepts(token) || !allowed()) return null
        var probe: GroupPathMeasurement? = null
        return try {
            val acquired = acquire { accepts(token) && allowed() }
            probe = acquired
            synchronized(lock) {
                if (!accepts(token) || !allowed()) return null
                measuring = acquired
            }
            acquired.measure().takeIf { it.isFinite() && it > 0 }
        } catch (_: Exception) { null }
        finally {
            synchronized(lock) { if (measuring === probe) measuring = null }
            try { probe?.close() } catch (_: Exception) {}
        }
    }

    /** Cancellation never opens a second worker while a slow acquisition is returning. */
    fun cancel() {
        val active = synchronized(lock) { generation++; measuring }
        try { active?.cancel() } catch (_: Exception) {}
    }

    fun awaitStopped() = worker.awaitTermination(3, java.util.concurrent.TimeUnit.SECONDS)

    fun isBusy() = synchronized(lock) { busy }

    override fun close() {
        synchronized(lock) { closed = true }
        cancel()
        worker.shutdown()
    }
}
