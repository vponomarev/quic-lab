package ru.vpnc.quiclab

/** Decision-only observer; the existing group session remains the route owner. */
internal class NormalGroupHealth(private val connections: List<String>) {
    init { require(connections.size in 1..4096 && connections.none { it.isBlank() }) }
    private data class Sample(val at: Long, val ms: Double)
    private val samples = Array(connections.size) { ArrayDeque<Sample>() }
    private var current = 0
    private var selectedAt = 0L
    private var activeSample: Sample? = null
    private var slowSince: Long? = null
    private var slowCount = 0
    private var queue = emptyList<List<Int>>()
    private var connectionCursor = 0
    private var nodeCursors = IntArray(0)

    fun reset(index: Int, now: Long) {
        require(index in connections.indices)
        current = index; selectedAt = now
        activeSample = null; slowSince = null; slowCount = 0
        samples.forEach { it.clear() }
        queue = connections.indices.filter { it != current }.groupBy { connections[it] }
            .entries.sortedBy { if (it.key == connections[current]) 1 else 0 }.map { it.value }
        connectionCursor = 0; nodeCursors = IntArray(queue.size)
    }

    fun active(now: Long, durationMs: Double?) {
        val previous = activeSample
        if (!valid(durationMs)) {
            activeSample = null; slowSince = null; slowCount = 0
            return
        }
        activeSample = Sample(now, durationMs!!)
        if (durationMs < 600.0) { slowSince = null; slowCount = 0; return }
        if (previous == null || now - previous.at !in 1..45000 || slowSince == null) {
            slowSince = now; slowCount = 1
        } else slowCount++
    }

    fun candidate(index: Int, now: Long, durationMs: Double?) {
        require(index in connections.indices)
        val values = samples[index]
        if (!valid(durationMs)) { values.clear(); return }
        if (values.lastOrNull()?.let { now - it.at !in 1..90000 } == true) values.clear()
        values.addLast(Sample(now, durationMs!!))
        while (values.size > 2) values.removeFirst()
    }

    private fun degraded(now: Long): Boolean {
        val latest = activeSample ?: return false
        val since = slowSince ?: return false
        return slowCount >= 3 && now - since >= 30000 && now - selectedAt >= 60000 && now - latest.at in 0..45000
    }

    private fun better(ms: Double) = activeSample?.let { ms <= it.ms * 0.6 && it.ms - ms >= 200.0 } == true

    fun replacement(now: Long): Int? {
        if (!degraded(now)) return null
        return samples.indices.filter { index ->
            index != current && samples[index].size == 2 && samples[index].all { now - it.at in 0..90000 && better(it.ms) }
        }.minByOrNull { index -> samples[index].maxOf { it.ms } }
    }

    fun nextCandidate(now: Long): Int? {
        if (queue.isEmpty()) return null
        // Confirm one promising path before spending the next round on a large catalogue.
        if (degraded(now)) samples.indices.firstOrNull { index ->
            index != current && samples[index].size == 1 && samples[index].last().let { now - it.at in 0..60000 && better(it.ms) }
        }?.let { return it }
        val group = connectionCursor
        connectionCursor = (connectionCursor + 1) % queue.size
        val nodes = queue[group]
        val node = nodes[nodeCursors[group] % nodes.size]
        nodeCursors[group] = (nodeCursors[group] + 1) % nodes.size
        return node
    }

    private fun valid(value: Double?) = value != null && value.isFinite() && value > 0
}
