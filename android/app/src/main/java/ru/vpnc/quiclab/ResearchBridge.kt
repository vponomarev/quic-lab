package ru.vpnc.quiclab

import android.content.Context
import mobile.ResearchObserver

internal object ResearchBridge {
 fun observer(context:Context):ResearchObserver {
  val app=context.applicationContext
  val owners=ResearchOwnerLookup(app)
  return object:ResearchObserver {
   override fun allowed()=ResearchSettings.allowed(app)
   override fun owner(protocol:Long,local:String,localPort:Long,remote:String,remotePort:Long):String {
    if(!allowed())return ""
    if(protocol !in 0L..255L || localPort !in 1L..65535L || remotePort !in 1L..65535L)return ""
    return owners.lookup(protocol.toInt(),local,localPort.toInt(),remote,remotePort.toInt())
   }
  }
 }
}
