package ru.vpnc.quiclab

import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import org.json.JSONObject

/** Publishes availability independently of the potentially blocked transport worker. */
internal class VpnUnderlyingNetworks(
    private val cm: ConnectivityManager,
    private val publish: (Array<Network>?) -> Boolean,
) : AutoCloseable {
    private val selection = VpnUnderlyingSelection<Network>()
    private val lost = mutableSetOf<Network>()
    private var requested: List<Network>? = null
    private var published = false
    private var last: List<Network>? = null
    private var closed = false

    private val physicalCallback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) = physicalChanged(network, true)
        override fun onLost(network: Network) = physicalChanged(network, false)
        override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) = refresh()
    }
    private val defaultCallback = object : ConnectivityManager.NetworkCallback() {
        override fun onAvailable(network: Network) = refresh()
        override fun onLost(network: Network) = refresh()
        override fun onCapabilitiesChanged(network: Network, caps: NetworkCapabilities) = refresh()
    }

    init {
        cm.registerNetworkCallback(NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN).build(), physicalCallback)
        try {
            cm.registerDefaultNetworkCallback(defaultCallback)
        } catch (e: Exception) {
            close()
            throw e
        }
    }

    /** null: active exit awaiting paths; empty: all exits explicitly paused. */
    @Synchronized fun update(networks: Array<Network>?) {
        if (closed) return
        requested = networks?.distinct()
        publishSelection(selection.update(requested, available(), cm.activeNetwork))
    }

    @Synchronized private fun physicalChanged(network: Network, up: Boolean) {
        if (closed) return
        if (up) lost.remove(network) else lost.add(network)
        refresh()
    }

    @Synchronized private fun refresh() {
        if (closed || requested == null) return
        publishSelection(selection.refresh(available(), cm.activeNetwork))
    }

    private fun available(): Set<Network> = requested.orEmpty().filter { network ->
        network !in lost && cm.getNetworkCapabilities(network)?.let {
            it.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
                !it.hasTransport(NetworkCapabilities.TRANSPORT_VPN)
        } == true
    }.toSet()

    private fun publishSelection(networks: List<Network>?) {
        if (published && last == networks) return
        if (publish(networks?.toTypedArray())) {
            last = networks
            published = true
            Diagnostics.event("vpn", JSONObject().put("event", "underlying_networks")
                .put("detail", networks?.joinToString(",") ?: "default"))
        }
    }

    @Synchronized override fun close() {
        if (closed) return
        closed = true
        runCatching { cm.unregisterNetworkCallback(physicalCallback) }
        runCatching { cm.unregisterNetworkCallback(defaultCallback) }
    }
}
