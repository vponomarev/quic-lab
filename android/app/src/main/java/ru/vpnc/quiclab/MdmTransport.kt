package ru.vpnc.quiclab

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.ParcelFileDescriptor
import mobile.Mobile
import mobile.MDMResolver
import mobile.SocketBinder
import java.io.Closeable
import java.net.Inet4Address
import java.util.concurrent.atomic.AtomicBoolean

/** One serialized exchange; network loss retires its native channel immediately.
 * Uses neither a VPN permission nor a process-global network binding. */
internal class MdmTransport(context:Context,private val secret:()->String,private val certificateAuthority:String=""):Closeable {
 private val app=context.applicationContext
 private val cm=app.getSystemService(ConnectivityManager::class.java)
 private val owner=VpnBudgetRun.sharedForContext(app)
 private val closed=AtomicBoolean(false)
 private val lock=Any()
 private var channel:mobile.MDMChannel?=null

 fun exchange(binding:MdmBinding,request:MdmSyncRequest):MdmSyncResponse {
  require(request.bindingId==binding.id && request.epoch==binding.epoch)
  return MdmSyncResponse.parse(post(binding,"sync",request.json().toString()))
 }
 fun post(binding:MdmBinding,operation:String,body:String):String {
  check(!closed.get()) { "MDM канал закрыт" }
  val network=cm.allNetworks.filter { n ->
   cm.getNetworkCapabilities(n)?.let { c ->
    c.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) &&
    !c.hasTransport(NetworkCapabilities.TRANSPORT_VPN) &&
    (c.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)||c.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR))
   }==true
  }.sortedBy { n -> if(cm.getNetworkCapabilities(n)?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)==true)0 else 1 }
   .firstOrNull()?:error("Нет физической сети для MDM")
  val caps=cm.getNetworkCapabilities(network)?:error("Сеть MDM исчезла")
  val physical=if(caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR))"cell" else "wifi"
  val budget=owner.acquireControl(VpnBudgetSettings.limitBytes(app))
  var native:mobile.MDMChannel?=null
  var registered=false
  val lost=AtomicBoolean(false)
  val callback=object:ConnectivityManager.NetworkCallback(){
   override fun onLost(n:Network){if(n==network){lost.set(true);synchronized(lock){channel?.close()}}}
  }
  try {
   val created=Mobile.newMDMChannel(budget,object:MDMResolver {
    override fun resolveAddress(host:String,port:Long):String {
     check(!closed.get())
     val ip=network.getAllByName(host).firstOrNull{it is Inet4Address}?:error("Нет IPv4 адреса MDM")
     return "${ip.hostAddress}:$port"
    }
   },certificateAuthority)
   native=created
   synchronized(lock){
    check(!closed.get());check(channel==null){"MDM запрос уже выполняется"};channel=created
   }
   cm.registerNetworkCallback(NetworkRequest.Builder().addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
    .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN).build(),callback)
   registered=true
   check(cm.getNetworkCapabilities(network)!=null){"Сеть MDM исчезла"}
   val binder=object:SocketBinder{
    override fun bind(fd:Long){
     check(!closed.get())
     ParcelFileDescriptor.fromFd(fd.toInt()).use{network.bindSocket(it.fileDescriptor)}
    }
   }
   val response=created.exchange(binding.url(operation),if(operation=="enroll")"" else secret(),body,budget.bind(binder,physical))
   check(!closed.get() && !lost.get())
   return response
  } finally {
   native?.close()
   synchronized(lock){if(channel===native)channel=null}
   if(registered)runCatching{cm.unregisterNetworkCallback(callback)}
   try{owner.checkpoint()}finally{owner.releaseControl()}
  }
 }
 override fun close(){closed.set(true);synchronized(lock){channel?.close()}}
}
