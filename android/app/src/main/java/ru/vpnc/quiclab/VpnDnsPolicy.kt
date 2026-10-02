package ru.vpnc.quiclab

import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.net.VpnService
import android.os.ParcelFileDescriptor
import java.net.Inet4Address
import mobile.MultiRouter
import mobile.SocketBinder
import org.json.JSONArray
import org.json.JSONObject

// Resolver sockets use a snapshot of the chosen physical network, never the VPN default.
internal class VpnDnsPolicy(private val service: VpnService, private val mode: String, private val exitId: String) : AutoCloseable {
 private val cm=service.getSystemService(ConnectivityManager::class.java)
 private var network: Network?=null
 private var kind="wifi"
 private var router: MultiRouter?=null
 private var closed=false
 private val callback=object:ConnectivityManager.NetworkCallback() {
  override fun onLinkPropertiesChanged(n:Network,lp:LinkProperties) {synchronized(this@VpnDnsPolicy) {if(n==network) publish()}}
  override fun onLost(n:Network) {synchronized(this@VpnDnsPolicy) {if(n==network) {network=null;publish()}}}
 }
 init {require(mode in listOf("tunnel","system"));cm.registerNetworkCallback(NetworkRequest.Builder().addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN).build(),callback)}
 @Synchronized fun snapshot(network:Network?):JSONObject = if(mode=="system") systemSnapshot(network?.let {cm.getLinkProperties(it)},kind) else tunnelSnapshot(exitId)
 @Synchronized fun attach(value:MultiRouter) {check(!closed);router=value;publish()}
 @Synchronized fun selected(value:Network?,networkKind:Int) {if(closed)return;network=value;kind=if(networkKind==VpnSession.CELLULAR) "cell" else "wifi";publish()}
 private fun publish() {if(closed)return;router?.updateDNS(snapshot(network).toString(),binder(network))}
 private fun binder(selected:Network?)=object:SocketBinder {
  override fun bind(fd:Long) {
   val physical=checkNotNull(selected) {"No physical DNS network"}
   check(service.protect(fd.toInt())) {"Cannot protect DNS socket"}
   ParcelFileDescriptor.fromFd(fd.toInt()).use {physical.bindSocket(it.fileDescriptor)}
  }
 }
 @Synchronized override fun close() {if(closed)return;closed=true;router=null;cm.unregisterNetworkCallback(callback);network=null}
 companion object {
  fun <T> selectBondResolver(available:Map<Int,T>,connected:Map<Int,T>,cellAllowed:Boolean):Pair<Int,T>? {
   for(kind in listOf(VpnSession.WIFI,VpnSession.CELLULAR)) {
    if(kind==VpnSession.CELLULAR && !cellAllowed) continue
    val network=available[kind] ?: continue
    if(connected[kind]==network) return kind to network
   }
   return null
  }
  fun tunnelSnapshot(id:String)=JSONObject().put("mode","tunnel").put("exit_id",id)
  fun systemSnapshot(lp:LinkProperties?,kind:String)=JSONObject().put("mode","system").put("network",kind).put("servers",JSONArray(lp?.dnsServers.orEmpty().filterIsInstance<Inet4Address>().map {it.hostAddress}))
 }
}
