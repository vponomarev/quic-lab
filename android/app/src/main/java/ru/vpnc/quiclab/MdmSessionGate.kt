package ru.vpnc.quiclab

import java.util.concurrent.atomic.AtomicBoolean

/** Controller cancellation is shared: a closed lease must drain before reuse. */
internal class MdmSessionGate {
 private val occupied=AtomicBoolean(false)
 fun open(factory:()->MdmSession?):MdmSession? {
  if(!occupied.compareAndSet(false,true))return null
  val source=try{factory()}catch(e:Throwable){occupied.set(false);throw e}
  if(source==null){occupied.set(false);return null}
  return object:MdmSession {
   private var closed=false
   private var running=false
   override fun isActive()=synchronized(this){!closed && source.isActive()}
   override fun sync(){
    synchronized(this){check(!closed && !running);running=true}
    try{source.sync()}finally{synchronized(this){running=false;if(closed)occupied.set(false)}}
   }
   override fun pause(){source.pause();close()}
   override fun close()=synchronized(this){
    if(!closed){closed=true;try{source.close()}finally{if(!running)occupied.set(false)}}
   }
  }
 }
}
