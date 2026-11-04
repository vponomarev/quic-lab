package ru.vpnc.quiclab

/** Coalesces job collisions without losing the poll owed to an explicit edit. */
internal class MdmQuietPassGate {
 private var running=false
 private var followup=false
 @Synchronized fun start():Boolean {
  if(running){followup=true;return false}
  running=true;return true
 }
 @Synchronized fun finish():Boolean {
  running=false
  val owed=followup;followup=false
  return owed
 }
 @Synchronized fun cancel(){followup=false}
}
