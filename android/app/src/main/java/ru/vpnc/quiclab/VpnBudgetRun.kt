package ru.vpnc.quiclab

import mobile.Mobile
import mobile.TrafficBudget
import java.util.UUID

/** Main-thread owner. Reconnecting exits must reuse current, never call stop. */
internal class VpnBudgetRun {
    var current: TrafficBudget? = null
        private set
    fun start(limitBytes: Long): TrafficBudget {
        require(limitBytes >= 0)
        return current ?: Mobile.newTrafficBudget(UUID.randomUUID().toString(), limitBytes).also { current = it }
    }
    fun stop() { current = null }
}
