package ru.vpnc.quiclab

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.UserManager

/** Never starts VPN. The durable lifecycle owner decides active versus paused. */
class MdmBootReceiver:BroadcastReceiver(){
 override fun onReceive(context:Context,intent:Intent){
  if(intent.action!=Intent.ACTION_BOOT_COMPLETED && intent.action!=Intent.ACTION_USER_UNLOCKED)return
  if(!context.getSystemService(UserManager::class.java).isUserUnlocked)return
  if(MdmService.owner==null)return
  runCatching{MdmService.start(context)}
 }
}
