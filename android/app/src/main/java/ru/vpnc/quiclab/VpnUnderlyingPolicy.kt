package ru.vpnc.quiclab

internal object VpnUnderlyingPolicy {
    /** null follows Android's default; empty explicitly means no active exit. */
    fun <T> select(requested: List<T>, available: Set<T>, default: T?): List<T>? {
        if (requested.isEmpty()) return emptyList()
        val live = requested.distinct().filter { it in available }
        if (live.isEmpty() || live.size == 1 && live[0] == default) return null
        return live.sortedBy { if (it == default) 0 else 1 }
    }
}

internal class VpnUnderlyingSelection<T> {
    private var requested: List<T>? = null
    private var followingDefault = false

    fun update(paths: List<T>?, available: Set<T>, default: T?): List<T>? {
        val distinct = paths?.distinct()
        if (distinct != requested) followingDefault = false
        requested = distinct
        return refresh(available, default)
    }

    fun refresh(available: Set<T>, default: T?): List<T>? {
        val paths = requested ?: return null
        // A default-network callback can precede the transport worker's selection.
        // Once following Android, never re-pin the same stale single path. A new
        // explicit selection, pause, or restored multipath list can leave this mode.
        if (followingDefault && paths.isNotEmpty() && paths.count { it in available } <= 1) return null
        return VpnUnderlyingPolicy.select(paths, available, default).also {
            followingDefault = it == null
        }
    }
}
