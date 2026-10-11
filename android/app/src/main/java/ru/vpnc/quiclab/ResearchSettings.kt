package ru.vpnc.quiclab

import android.content.Context
import java.util.concurrent.CopyOnWriteArraySet

/** Device consent only. The tunnel separately negotiates the server collection policy. */
internal object ResearchSettings {
 const val explanation="Передача серверу идентичности приложений, сведений об их соединениях и DNS. Разрешение также охватывает полный серверный захват собственного QUIC/HTTPS-туннеля устройства. Прямой трафик требует дополнительного разрешения и выбора охвата. Расширенный анализ при AWG/VLESS пока недоступен."
 private val listeners=CopyOnWriteArraySet<()->Unit>()
 fun allowed(context:Context):Boolean=runCatching{allowed(MdmStore(context).read())}.getOrDefault(false)
 internal fun allowed(state:MdmState):Boolean {
  if(state.cleanupPending || state.pendingEnrollment)return false
  if(state.binding!=null)return state.active && state.rights.research
  return state.standaloneResearch
 }
 internal fun directAllowed(state:MdmState):Boolean = allowed(state) &&
  (if(state.binding!=null)state.rights.researchDirect else state.standaloneResearchDirect)
 internal fun effectiveScope(state:MdmState):ResearchScope =
  if(directAllowed(state))state.researchScope else state.researchScope.vpnOnly()
 fun supported(context:Context):Boolean=runCatching{
  val ids=if(VpnProfiles.multiple(context))VpnProfiles.enabled(context)else setOf(VpnProfiles.current(context).id)
  ids.isNotEmpty() && ids.all{VpnProfiles.preferences(context,it).getString("transport","quic") in setOf("quic","https")}
 }.getOrDefault(false)
 fun effectiveScope(context:Context):ResearchScope=runCatching{
  val scope=effectiveScope(MdmStore(context).read())
  if(supported(context))scope else scope.vpnOnly()
 }.getOrDefault(ResearchScope())
 fun setStandalone(context:Context,enabled:Boolean){
  MdmStore(context).setStandaloneResearch(enabled)
  changed()
 }
 /** Called after durable consent/authority changes, including intermediate resume revocation.
  * Callbacks must be fast and nonblocking: post work to the tunnel's serial controller. */
 internal fun changed(){listeners.forEach{runCatching{it()}}}
 fun observeCurrent(listener:()->Unit):AutoCloseable {
  val guard=Any();var closed=false
  val serial:()->Unit={synchronized(guard){if(!closed)listener()}}
  listeners.add(serial)
  try{serial()}catch(e:Exception){listeners.remove(serial);throw e}
  return AutoCloseable{synchronized(guard){closed=true;listeners.remove(serial)}}
 }
 fun observe(listener:()->Unit):AutoCloseable {
  listeners.add(listener)
  return AutoCloseable{listeners.remove(listener)}
 }
}
