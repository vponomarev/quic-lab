package ru.vpnc.quiclab

/** Conservative preference changes; urgent failover does not wait for dwell. */
internal class MigrationPolicy {
    fun isStalled(silenceMS: Long, rttMS: Double, intervalMS: Long): Boolean =
        silenceMS > maxOf(400L, (rttMS * 4).toLong().coerceAtMost(2000L), intervalMS * 3)

    fun canPreferWifi(healthyMS: Long, dwellMS: Long) = healthyMS >= 5000 && dwellMS >= 8000

    fun retryDelay(failures: Int): Long = (1000L shl failures.coerceIn(0, 5)).coerceAtMost(30000)
}
