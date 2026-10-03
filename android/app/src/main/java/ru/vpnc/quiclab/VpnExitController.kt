package ru.vpnc.quiclab

/** Owns sessions, and serializes generation checks with callback side effects. */
internal class VpnExitController<T: AutoCloseable>(
 private val ids: Set<String>,
 private val factory: (String,Long)->T,
) {
 private var serial=0L
 private var closed=false
 private val generations=mutableMapOf<String,Long>()
 private val sessions=mutableMapOf<String,T>()
 private val states=mutableMapOf<String,String>()
 @Synchronized fun start(id:String):Long {
  check(!closed);require(id in ids)
  stop(id)
  val token=++serial
  generations[id]=token;states[id]="connecting"
  try {sessions[id]=factory(id,token)} catch(e:Exception) {
   generations.remove(id);states[id]="blocked";throw e
  }
  return token
 }
 @Synchronized fun update(id:String,paused:Boolean,refresh:()->Unit):Long? {
  check(!closed);require(id in ids);refresh();return if(paused) null else start(id)
 }
 @Synchronized fun current(id:String,token:Long)=!closed && generations[id]==token
 @Synchronized fun withCurrent(id:String,token:Long,action:()->Unit):Boolean {
  if(!current(id,token))return false
  action();return true
 }
 @Synchronized fun apply(id:String,token:Long,kind:String):Boolean {
  if(!current(id,token) || kind !in setOf("active","recovering","blocked","incompatible"))return false
  states[id]=kind
  if(kind=="incompatible") {generations.remove(id);sessions.remove(id)?.close()}
  return true
 }
 fun event(id:String,token:Long,kind:String) {
  val state=when(kind) {
   "connected" -> "active"
   "disconnected", "session_closed", "operation_failed" -> "recovering"
   "incompatible" -> "incompatible"
   else -> return
  }
  apply(id,token,state)
 }
 @Synchronized fun token(id:String):Long?=generations[id]
 @Synchronized fun state(id:String)=states[id] ?: "stopped"
 @Synchronized fun session(id:String):T?=sessions[id]
 @Synchronized fun stop(id:String) {
  generations.remove(id);states[id]="stopped"
  sessions.remove(id)?.close()
 }
 @Synchronized fun stopAll() {
  closed=true
  generations.clear()
  ids.forEach { states[it]="stopped" }
  val pending=sessions.values.toList();sessions.clear()
  var failure:Exception?=null
  pending.forEach { try {it.close()}catch(e:Exception){if(failure==null)failure=e else failure!!.addSuppressed(e)} }
  failure?.let {throw it}
 }
}