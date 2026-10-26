package ru.vpnc.quiclab
import android.net.*
import android.os.Bundle
import androidx.test.platform.app.InstrumentationRegistry
import org.json.*
import org.junit.Test
import org.junit.Assume.assumeTrue
import java.net.*
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit

/** Explicit opt-in, small UDP probe; never relays payload via rendezvous. */
class LtePeerProbeTest {
 @Test fun probe(){
  val args=InstrumentationRegistry.getArguments();val token=args.getString("peerToken")
  assumeTrue(token!=null)
  val id=args.getString("peerId")!!;val host=args.getString("peerHost")?:"45.9.88.105";val direct=args.getString("directPeer")
  val inst=InstrumentationRegistry.getInstrumentation()
  fun report(j:JSONObject){inst.sendStatus(0,Bundle().apply{putString("stream",j.toString()+"\n")})}
  val cm=inst.targetContext.applicationContext.getSystemService(ConnectivityManager::class.java)
  val queue=LinkedBlockingQueue<Network>()
  val cb=object:ConnectivityManager.NetworkCallback(){override fun onAvailable(n:Network){queue.offer(n)}}
  cm.requestNetwork(NetworkRequest.Builder().addTransportType(NetworkCapabilities.TRANSPORT_CELLULAR).addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET).addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN).build(),cb)
  try{
   val net=queue.poll(25,TimeUnit.SECONDS)?:error("No cellular bearer")
   val caps=cm.getNetworkCapabilities(net)!!;check(caps.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR)&&!caps.hasTransport(NetworkCapabilities.TRANSPORT_VPN))
   val lp=cm.getLinkProperties(net)!!
   DatagramSocket(null).use{s->
    s.bind(InetSocketAddress(if(direct==null)0 else 39095));net.bindSocket(s);s.soTimeout=70
    val local=lp.linkAddresses.map{it.address}.filter{!it.isLoopbackAddress&&!it.isLinkLocalAddress}.map{it.hostAddress!!}
    report(JSONObject().put("event","network").put("id",id).put("interface",lp.interfaceName).put("addresses",JSONArray(local)).put("port",s.localPort).put("network",net.toString()))
    var candidates=if(direct==null)JSONArray() else JSONArray().put(JSONObject().put("label","direct-offline").put("host",direct).put("port",39095));val received=mutableMapOf<String,Int>();val acks=mutableMapOf<String,Int>();val rtts=mutableMapOf<String,MutableList<Long>>();var sent=0;var serverReplies=0;var errors=0
    val start=android.os.SystemClock.elapsedRealtime();var lastRegister=0L;var lastProbe=0L
    fun send(j:JSONObject,address:InetSocketAddress){val bytes=j.toString().toByteArray();try{s.send(DatagramPacket(bytes,bytes.size,address));sent++}catch(_:Exception){errors++}}
    while(android.os.SystemClock.elapsedRealtime()-start<55000){
     val now=android.os.SystemClock.elapsedRealtime()
     if(direct==null && now-lastRegister>=1000){
      val hello=JSONObject().put("token",token).put("id",id).put("type","register").put("local",JSONArray(local)).put("port",s.localPort)
      for(p in listOf(39093,39094))send(hello,InetSocketAddress(host,p));lastRegister=now
     }
     if(now-lastProbe>=250){for(i in 0 until candidates.length()){
      val c=candidates.getJSONObject(i)
      send(JSONObject().put("token",token).put("id",id).put("type","probe").put("path",c.getString("label")).put("time",now),InetSocketAddress(c.getString("host"),c.getInt("port")))
     };lastProbe=now}
     val packet=DatagramPacket(ByteArray(2048),2048)
     try{s.receive(packet)}catch(_:SocketTimeoutException){continue}
     val j=runCatching{JSONObject(String(packet.data,0,packet.length))}.getOrNull()?:continue
     if(j.optString("token")!=token)continue
     if(j.optString("type")=="peers"&&packet.address.hostAddress==host){
      serverReplies++;candidates=j.getJSONArray("candidates")
      if(serverReplies==1||serverReplies==4)report(j.put("event","rendezvous"));continue
     }
     if(j.optString("id")==id)continue
     val key=j.optString("path")+" from "+packet.address.hostAddress+":"+packet.port
     if(j.optString("type")=="probe"){
      received[key]=(received[key]?:0)+1
      send(JSONObject().put("token",token).put("id",id).put("type","ack").put("path",j.getString("path")).put("time",j.getLong("time")),InetSocketAddress(packet.address,packet.port))
     }else if(j.optString("type")=="ack"){
      acks[key]=(acks[key]?:0)+1;rtts.getOrPut(key){mutableListOf()}.add(android.os.SystemClock.elapsedRealtime()-j.getLong("time"))
     }
    }
    val timings=JSONObject();rtts.forEach{(k,v)->timings.put(k,JSONObject().put("minMs",v.minOrNull()).put("medianMs",v.sorted()[v.size/2]))}
    report(JSONObject().put("event","result").put("id",id).put("sent",sent).put("serverReplies",serverReplies).put("errors",errors).put("received",JSONObject(received)).put("acks",JSONObject(acks)).put("rtt",timings).put("candidates",candidates))
    check(direct!=null || serverReplies>0){"Rendezvous unreachable; P2P result inconclusive"}
   }
  }finally{cm.unregisterNetworkCallback(cb)}
 }
 @Test fun imsAccess(){
  assumeTrue(InstrumentationRegistry.getArguments().getString("peerToken")!=null)
  val inst=InstrumentationRegistry.getInstrumentation();val cm=inst.targetContext.applicationContext.getSystemService(ConnectivityManager::class.java)
  fun out(j:JSONObject){inst.sendStatus(0,Bundle().apply{putString("stream",j.toString()+"\n")})}
  val visible=cm.allNetworks.filter{cm.getNetworkCapabilities(it)?.hasCapability(NetworkCapabilities.NET_CAPABILITY_IMS)==true}
  out(JSONObject().put("event","ims-visible").put("count",visible.size))
  for(n in visible){
   val p=cm.getLinkProperties(n)
   val j=JSONObject().put("event","ims-bind").put("network",n.toString()).put("addresses",JSONArray(p?.linkAddresses?.map{it.address.hostAddress}?:emptyList<String>()))
   try{DatagramSocket().use{n.bindSocket(it)};j.put("bound",true)}catch(e:Exception){j.put("bound",false).put("error",e.toString())};out(j)
  }
  val q=LinkedBlockingQueue<Network>();val cb=object:ConnectivityManager.NetworkCallback(){override fun onAvailable(n:Network){q.offer(n)}}
  var registered=false
  try{
   cm.requestNetwork(NetworkRequest.Builder().addTransportType(NetworkCapabilities.TRANSPORT_CELLULAR).addCapability(NetworkCapabilities.NET_CAPABILITY_IMS).removeCapability(NetworkCapabilities.NET_CAPABILITY_NOT_RESTRICTED).build(),cb);registered=true
   val n=q.poll(5,TimeUnit.SECONDS)
   out(JSONObject().put("event","ims-request").put("available",n!=null))
  }catch(e:Exception){out(JSONObject().put("event","ims-request").put("error",e.toString()))}
  finally{if(registered)cm.unregisterNetworkCallback(cb)}
 }
}
