package ru.vpnc.quiclab

import java.net.URI
import java.net.URLDecoder
import java.util.concurrent.atomic.AtomicBoolean

internal data class TemporaryEditorInvitation(val endpoint:String,val id:String,val token:String,val minutes:Int){
 companion object{
  fun parse(raw:String):TemporaryEditorInvitation{
   require(raw.length<=4096)
   val uri=URI(raw);require(uri.scheme=="quiclab"&&uri.host=="editor"&&uri.path=="/pair"&&uri.rawQuery==null&&uri.userInfo==null&&uri.port==-1)
   val fields=linkedMapOf<String,String>()
   for(part in (uri.rawFragment?:error("Нет параметров")).split('&')){
    val pair=part.split('=',limit=2);require(pair.size==2)
    val k=URLDecoder.decode(pair[0],"UTF-8");require(k !in fields)
    fields[k]=URLDecoder.decode(pair[1],"UTF-8")
   }
   require(fields.keys==setOf("server","id","token","minutes"))
   val endpoint=URI(fields.getValue("server"));require(endpoint.scheme=="https"&&endpoint.host!=null&&endpoint.userInfo==null&&endpoint.rawQuery==null&&endpoint.rawFragment==null&&endpoint.path.isNullOrEmpty()&&endpoint.port in -1..65535&&endpoint.port!=0)
   val id=fields.getValue("id");val token=fields.getValue("token");require(id.matches(Regex("[0-9a-f]{64}"))&&token.matches(Regex("[0-9a-f]{64}")))
   val minutes=fields.getValue("minutes").toInt();require(minutes in listOf(15,30,60))
   return TemporaryEditorInvitation(endpoint.toASCIIString(),id,token,minutes)
  }
 }
}
/** elapsedRealtime is monotonic and includes device sleep. Nothing is persisted. */
internal class TemporaryEditorLease(minutes:Int,private val now:()->Long={android.os.SystemClock.elapsedRealtime()}){
 private val revoked=AtomicBoolean(false)
 private val deadline=now()+minutes*60000L
 fun revoke(){revoked.set(true)}
 fun valid():Boolean{if(now()>=deadline)revoke();return !revoked.get()}
 fun remainingSeconds():Long=if(valid())((deadline-now())/1000).coerceAtLeast(0) else 0
}
