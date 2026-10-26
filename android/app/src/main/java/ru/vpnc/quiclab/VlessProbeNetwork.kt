package ru.vpnc.quiclab
import android.content.Context
import android.net.*
import android.os.ParcelFileDescriptor
import android.os.CancellationSignal
import java.net.InetAddress
import java.net.Inet4Address
import java.net.Inet6Address
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference
internal class ProbeNetworkLease(val network:Network,val kind:String,val budget:mobile.TrafficBudget,val binder:mobile.SocketBinder,private val owner:VpnBudgetRun,private val releaseNetwork:()->Unit={}):AutoCloseable{
 private val closed=AtomicBoolean(false)
 override fun close(){if(!closed.compareAndSet(false,true))return;try{owner.checkpoint()}finally{try{owner.releaseControl()}finally{releaseNetwork()}}}
 fun resolve(host:String,deadline:Long):String{
  if(InetAddresses.isNumericAddress(host)){val address=InetAddresses.parseNumericAddress(host);return when(address){is Inet4Address,is Inet6Address->address.hostAddress;else->throw java.net.UnknownHostException()}}
  val latch=CountDownLatch(1);val signal=CancellationSignal();var answers:List<InetAddress> = emptyList()
  DnsResolver.getInstance().query(network,host,DnsResolver.FLAG_EMPTY,java.util.concurrent.Executor{it.run()},signal,object:DnsResolver.Callback<List<InetAddress>>{
   override fun onAnswer(answer:List<InetAddress>,rcode:Int){answers=answer;latch.countDown()}
   override fun onError(error:DnsResolver.DnsException){latch.countDown()}
  })
  try{if(!latch.await((deadline-android.os.SystemClock.elapsedRealtime()).coerceAtLeast(1),TimeUnit.MILLISECONDS))throw java.net.SocketTimeoutException();return (answers.firstOrNull{it is Inet4Address}?:answers.firstOrNull{it is Inet6Address})?.hostAddress?:throw java.net.UnknownHostException()}finally{signal.cancel()}
 }
}
internal object VlessProbeNetwork{
 fun selectedKind(c:Context):String? {
  val network=selected(c)?:return null
  val caps=c.getSystemService(ConnectivityManager::class.java).getNetworkCapabilities(network)?:return null
  return if(caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR))"cell" else "wifi"
 }
 fun available(c:Context,network:Network,kind:String):Boolean = matches(c.getSystemService(ConnectivityManager::class.java),network,kind)
 private fun matches(cm:ConnectivityManager,network:Network,kind:String):Boolean = cm.getNetworkCapabilities(network)?.let {
  it.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) && !it.hasTransport(NetworkCapabilities.TRANSPORT_VPN) &&
   it.hasTransport(if(kind=="cell")NetworkCapabilities.TRANSPORT_CELLULAR else NetworkCapabilities.TRANSPORT_WIFI)
 }==true
 /** Explicit physical network; cellular request is retained until the phase has drained. */
 fun acquire(c:Context,kind:String,canceled:()->Boolean={false}):ProbeNetworkLease {
  require(kind=="wifi" || kind=="cell")
  fun checkCanceled(){if(canceled() || Thread.currentThread().isInterrupted)throw InterruptedException("canceled")}
  checkCanceled()
  val cm=c.getSystemService(ConnectivityManager::class.java)
  val owner=VpnBudgetRun.sharedForContext(c)
  val meter=owner.acquireControl(VpnBudgetSettings.limitBytes(c))
  var callback:ConnectivityManager.NetworkCallback?=null
  var registered=false
  fun release(){if(registered){registered=false;callback?.let{runCatching{cm.unregisterNetworkCallback(it)}}}}
  try {
   if(kind=="cell")check(meter.cellAllowed()){"budget"}
   checkCanceled()
   val network=if(kind=="wifi") {
    cm.allNetworks.firstOrNull{matches(cm,it,kind)}?:error("no_network")
   } else {
    val candidate=AtomicReference<Network?>(null)
    val unavailable=AtomicBoolean(false)
    val ready=CountDownLatch(1)
    val requested=object:ConnectivityManager.NetworkCallback(){
     override fun onAvailable(network:Network){candidate.set(network);ready.countDown()}
     override fun onLost(network:Network){candidate.compareAndSet(network,null)}
     override fun onUnavailable(){unavailable.set(true);ready.countDown()}
    }
    callback=requested
    cm.requestNetwork(NetworkRequest.Builder().addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET).addTransportType(NetworkCapabilities.TRANSPORT_CELLULAR).build(),requested,10000)
    registered=true
    val deadline=android.os.SystemClock.elapsedRealtime()+10000
    var found:Network?=null
    while(found==null){
     checkCanceled()
     check(meter.cellAllowed()){"budget"}
     found=candidate.get()?.takeIf{matches(cm,it,kind)}
     if(found!=null)break
     if(unavailable.get() || android.os.SystemClock.elapsedRealtime()>=deadline)error("no_network")
     // onAvailable may precede visible capabilities; avoid spinning after the latch opens.
     if(ready.count>0)ready.await(100,TimeUnit.MILLISECONDS) else Thread.sleep(100)
    }
    checkNotNull(found){"no_network"}
   }
   checkCanceled()
   check(matches(cm,network,kind)){"no_network"}
   return lease(cm,network,kind,meter,owner,::release)
  } catch(error:Throwable){
   release()
   try{owner.releaseControl()}catch(cleanup:Throwable){error.addSuppressed(cleanup)}
   throw error
  }
 }
 private fun lease(cm:ConnectivityManager,network:Network,kind:String,meter:mobile.TrafficBudget,owner:VpnBudgetRun,release:()->Unit={}):ProbeNetworkLease =
  ProbeNetworkLease(network,kind,meter,object:mobile.SocketBinder{override fun bind(fd:Long){
   check(matches(cm,network,kind)){"no_network"}
   if(kind=="cell")check(meter.cellAllowed()){"budget"}
   check(LabVpnService.protectProbe(fd))
   ParcelFileDescriptor.fromFd(fd.toInt()).use{network.bindSocket(it.fileDescriptor)}
  }},owner,release)
 fun selected(c:Context):Network?{
  val cm=c.getSystemService(ConnectivityManager::class.java)
  val networks=cm.allNetworks.filter{n->cm.getNetworkCapabilities(n)?.let{it.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)&&!it.hasTransport(NetworkCapabilities.TRANSPORT_VPN)&&(it.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)||it.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR))}==true}
  val active=LabVpnService.probeNetwork()
  return networks.firstOrNull{it==active}?:networks.firstOrNull{it==cm.activeNetwork}?:networks.firstOrNull{cm.getNetworkCapabilities(it)?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI)==true}?:networks.firstOrNull()
 }
 fun acquire(c:Context):ProbeNetworkLease{
  val cm=c.getSystemService(ConnectivityManager::class.java)
  val network=selected(c)?:error("no_network")
  val kind=if(cm.getNetworkCapabilities(network)!!.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR))"cell" else "wifi"
  val owner=VpnBudgetRun.sharedForContext(c);val meter=owner.acquireControl(VpnBudgetSettings.limitBytes(c))
  return lease(cm,network,kind,meter,owner)
 }
}
