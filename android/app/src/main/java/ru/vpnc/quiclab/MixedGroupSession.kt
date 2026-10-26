package ru.vpnc.quiclab

/** Owns independently acquired group clients behind the stable Android router. */
internal class MixedGroupSession<T>(
    val policy: VlessGroupPolicy,
    private val attach: (T) -> Unit,
    private val detach: (T) -> Unit,
    private val dispose: (T) -> Unit,
) : AutoCloseable {
    private val lock = Any()
    @Volatile private var closed = false
    @Volatile var generation: Long = 0; private set
    private var active: T? = null
    private var lastHealthAt = 0L
    private var nextFailureAt = 0L

    fun accepts(token: Long): Boolean = !closed && token == generation

    /** Acquisition can block. Close invalidates its token without waiting for the dial. */
    fun replace(index: Int, now: Long, acquire: (Long) -> T): Boolean {
        val previous: T?
        val token: Long
        synchronized(lock) {
            if (closed) return false
            token = ++generation
            previous = active
            active = null
        }
        // In particular, stop an AWG peer before starting a replacement of the same peer.
        retire(previous)
        var candidate: T? = null
        try {
            val acquired = acquire(token)
            candidate = acquired
            synchronized(lock) {
                if (!accepts(token)) { dispose(acquired); return false }
                try { attach(acquired) }
                catch (failure: Exception) {
                    // Attach may have published the new gateway before throwing.
                    try { detach(acquired) } catch (_: Exception) {}
                    throw failure
                }
                active = acquired
                policy.select(index)
                lastHealthAt = now
                nextFailureAt = now
                return true
            }
        } catch (_: Exception) {
            candidate?.let { dispose(it) }
            synchronized(lock) {
                if (accepts(token)) {
                    policy.deferRetry(index, now)
                    // A failed final candidate may still have queued native connected events.
                    ++generation
                }
            }
            return false
        }
    }

    fun healthy(token: Long, now: Long) {
        if (!accepts(token) || active == null) return
        lastHealthAt = now
        nextFailureAt = now
        policy.healthy()
    }

    fun failed(token: Long, now: Long): Int? {
        if (!accepts(token) || active == null || now < nextFailureAt) return null
        nextFailureAt = now + 5000
        return policy.failed(now)
    }

    /** Engine initialization, especially AWG, is never evidence of tunnel health. */
    fun check(now: Long, connected: Boolean): Int? {
        if (closed) return null
        if (active == null) return policy.retry(now)
        if (!connected || now - lastHealthAt >= 15000) return failed(generation, now)
        return policy.retry(now)
    }

    private fun retire(previous: T?) {
        previous ?: return
        try { detach(previous) } finally { dispose(previous) }
    }

    fun stop() {
        val previous = synchronized(lock) { ++generation; val old = active; active = null; old }
        retire(previous)
    }

    override fun close() {
        synchronized(lock) { closed = true }
        stop()
    }
}
