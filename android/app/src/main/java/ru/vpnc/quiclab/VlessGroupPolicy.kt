package ru.vpnc.quiclab

/** Serialized by VpnSession worker. Candidate selection never changes active identity. */
internal class VlessGroupPolicy(private val size:Int,private val connections:List<String> = List(size){"subscription"}){
 init{require(size in 1..4096 && connections.size==size && connections.none{it.isBlank()})}
 var current=0;private set
 private var cursor=0
 private var failures=0
 private val retryAfter=LongArray(size)
 private val failedInRound=BooleanArray(size)
 fun healthy(){failures=0;retryAfter[current]=0;failedInRound.fill(false)}
 fun resetNetwork(){retryAfter.fill(0);failedInRound.fill(false);failures=0;cursor=current}
 fun select(index:Int){require(index in 0 until size);current=index;cursor=index;failures=0}
 fun deferRetry(index:Int,now:Long){failures=2;failedInRound[index]=true;retryAfter[index]=now+30000}
 fun exhausted(now:Long)=failedInRound.all{it} || (failures>=2 && retryAfter.all{it>now})
 fun failed(now:Long):Int? {failures++;if(failures<2)return null;failedInRound[current]=true;retryAfter[current]=now+30000;return retry(now)}
 fun retry(now:Long):Int? {
  if(failures<2)return null
  // Retry another logical connection before consuming another node of a large source.
  for(other in listOf(true,false))for(step in 1..size){
   val index=(cursor+step)%size
   if((connections[index]!=connections[cursor])==other && retryAfter[index]<=now){cursor=index;return index}
  }
  return null
 }
}
