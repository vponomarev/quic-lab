package ru.vpnc.quiclab

import android.app.*
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.*

/** A visible foreground session; Android/process restart never restores authority. */
class TemporaryEditorService:Service(){
 private var worker:Thread?=null
 private val handler=Handler(Looper.getMainLooper())
 private val tick=object:Runnable{override fun run(){
  val s=TemporaryEditorRuntime.current
  if(s==null||!s.valid()){TemporaryEditorRuntime.stop();stopSelf();return}
  handler.postDelayed(this,1000)
 }}
 override fun onBind(intent:Intent?):IBinder?=null
 override fun onStartCommand(intent:Intent?,flags:Int,startId:Int):Int{
  if(intent?.action=="stop"){TemporaryEditorRuntime.stop();stopSelf();return START_NOT_STICKY}
  val s=TemporaryEditorRuntime.current?:run{stopSelf();return START_NOT_STICKY}
  if(worker!=null)return START_NOT_STICKY
  val manager=getSystemService(NotificationManager::class.java)
  manager.createNotificationChannel(NotificationChannel("temporary_editor","Временный WEB-редактор",NotificationManager.IMPORTANCE_LOW))
  val open=PendingIntent.getActivity(this,304,Intent(this,TemporaryEditorActivity::class.java),PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
  val stop=PendingIntent.getService(this,305,Intent(this,TemporaryEditorService::class.java).setAction("stop"),PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
  val n=Notification.Builder(this,"temporary_editor").setSmallIcon(android.R.drawable.ic_menu_edit).setContentTitle("Открыт доступ к настройкам")
   .setContentText("WEB-редактор · ${java.net.URI(s.invitation.endpoint).host}").setContentIntent(open).setOngoing(true)
   .addAction(Notification.Action.Builder(null,"Завершить",stop).build()).build()
  if(Build.VERSION.SDK_INT>=34)startForeground(304,n,ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)else startForeground(304,n)
  handler.post(tick)
  worker=Thread({
   try{while(s.valid()){
    try{s.sync()}catch(e:Exception){if(e.message?.contains("HTTP status 401")==true){s.close();break};if(!s.valid())break;try{s.checkAuthority()}catch(_:Exception){break}}
    Thread.sleep(3000)
   }}catch(_:InterruptedException){}finally{handler.post{if(TemporaryEditorRuntime.current===s)TemporaryEditorRuntime.stop();stopSelf()}}
  },"temporary-editor").also{it.start()}
  return START_NOT_STICKY
 }
 override fun onDestroy(){handler.removeCallbacks(tick);TemporaryEditorRuntime.stop();worker?.interrupt();stopForeground(STOP_FOREGROUND_REMOVE);super.onDestroy()}
}
