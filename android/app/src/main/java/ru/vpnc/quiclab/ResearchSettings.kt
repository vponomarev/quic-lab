package ru.vpnc.quiclab

import android.content.Context
import java.util.concurrent.CopyOnWriteArraySet

/** Device consent only. The tunnel separately negotiates the server collection policy. */
internal object ResearchSettings {
 const val explanation="Передача серверу идентичности приложений, сведений об их соединениях и DNS. Разрешение также охватывает полный серверный захват собственного QUIC/HTTPS-туннеля устройства. Прямой трафик, AWG и VLESS не исследуются."
 private val listeners=CopyOnWriteArraySet<()->Unit>()
 fun allowed(context:Context):Boolean=runCatching{allowed(MdmStore(context).read())}.getOrDefault(false)
 internal fun allowed(state:MdmState):Boolean {
  if(state.cleanupPending || state.pendingEnrollment)return false
  if(state.binding!=null)return state.active && state.rights.research
  return state.standaloneResearch
 }
 fun setStandalone(context:Context,enabled:Boolean){
  MdmStore(context).setStandaloneResearch(enabled)
  changed()
 }
 /** Called after durable consent/authority changes, including intermediate resume revocation.
  * Callbacks must be fast and nonblocking: post work to the tunnel's serial controller. */
 internal fun changed(){listeners.forEach{runCatching{it()}}}
 fun observe(listener:()->Unit):AutoCloseable {
  listeners.add(listener)
  return AutoCloseable{listeners.remove(listener)}
 }
}
