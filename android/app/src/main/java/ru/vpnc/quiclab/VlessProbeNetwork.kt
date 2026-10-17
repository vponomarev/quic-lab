package ru.vpnc.quiclab
import android.content.Context
import android.net.*
import android.os.ParcelFileDescriptor
import android.os.CancellationSignal
import java.net.InetAddress
import java.net.Inet4Address
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
internal class ProbeNetworkLease(val network:Network,val kind:String,val budget:mobile.TrafficBudget,val binder:mobile.SocketBinder,private val owner:VpnBudgetRun):AutoCloseable{
 override fun close(){try{owner.checkpoint()}finally{owner.releaseControl()}}
 fun resolve(host:String,deadline:Long):String{
  if(InetAddresses.isNumericAddress(host)){val address=InetAddresses.parseNumericAddress(host);return (address as? Inet4Address)?.hostAddress?:throw java.net.UnknownHostException()}
  val latch=CountDownLatch(1);val signal=CancellationSignal();var answers:List<InetAddress> = emptyList()
  DnsResolver.getInstance().query(network,host,DnsResolver.FLAG_EMPTY,java.util.concurrent.Executor{it.run()},signal,object:DnsResolver.Callback<List<InetAddress>>{
   override fun onAnswer(answer:List<InetAddress>,rcode:Int){answers=answer;latch.countDown()}
   override fun onError(error:DnsResolver.DnsException){latch.countDown()}
  })
  try{if(!latch.await((deadline-android.os.SystemClock.elapsedRealtime()).coerceAtLeast(1),TimeUnit.MILLISECONDS))throw java.net.SocketTimeoutException();return answers.firstOrNull{it is Inet4Address}?.hostAddress?:throw java.net.UnknownHostException()}finally{signal.cancel()}
 }
}
internal object VlessProbeNetwork{
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
  return ProbeNetworkLease(network,kind,meter,object:mobile.SocketBinder{override fun bind(fd:Long){check(cm.getNetworkCapabilities(network)!=null);check(LabVpnService.protectProbe(fd));ParcelFileDescriptor.fromFd(fd.toInt()).use{network.bindSocket(it.fileDescriptor)}}},owner)
 }
}
