package ru.vpnc.quiclab

import android.app.*
import android.content.*
import android.net.Uri
import android.os.Environment
import android.provider.MediaStore
import java.io.File

/** Persistent transfer state. A killed process becomes an explicit retry, never an endless pending job. */
internal object OwnedApkDownloads {
 @Volatile private var activeId=0L
 private fun prefs(c:Context)=c.getSharedPreferences("apk_download",Context.MODE_PRIVATE)
 fun owns(c:Context)=prefs(c).getBoolean("owned",false)
 fun active(c:Context,id:Long)=activeId==id && prefs(c).getLong("id",0)==id
 fun uri(c:Context):Uri?=prefs(c).getString("uri",null)?.let{Uri.parse(it)}
 fun file(c:Context,id:Long)=File(c.cacheDir,"apk-transfer-$id.part")
 @Synchronized fun update(c:Context,id:Long,status:Int,bytes:Long,total:Long,message:String):Boolean {
  val p=prefs(c);if(p.getLong("id",0)!=id || !active(c,id))return false
  return p.edit().putInt("status",status).putLong("bytes",bytes).putLong("total",total).putString("message",message).commit()
 }
 @Synchronized fun start(c:Context,offer:ApplicationUpdates.Offer){
  val p=prefs(c);val id=System.currentTimeMillis().coerceAtLeast(p.getLong("id",0)+1)
  check(p.edit().clear().putBoolean("owned",true).putLong("id",id).putString("url",offer.url)
   .putString("name",offer.name).putInt("code",offer.code).putInt("status",DownloadManager.STATUS_PENDING)
   .putLong("total",-1).putString("message","Начинаем скачивание…").commit()){"Не удалось сохранить загрузку"}
  activeId=id
  try {c.startForegroundService(Intent(c,ApkTransferService::class.java).putExtra("id",id))}
  catch(e:Exception){update(c,id,DownloadManager.STATUS_FAILED,0,-1,"Не удалось запустить загрузку: ${e.message}");activeId=0;throw e}
 }
 @Synchronized fun snapshot(c:Context):ApkDownloads.State {
  val p=prefs(c);val id=p.getLong("id",0);var status=p.getInt("status",0)
  if(id>0 && status in listOf(DownloadManager.STATUS_PENDING,DownloadManager.STATUS_RUNNING) && activeId!=id){
   status=DownloadManager.STATUS_FAILED
   p.edit().putInt("status",status).putString("message","Загрузка прервана. Нажмите «Повторить скачивание»").commit()
  }
  return ApkDownloads.State(id,status,p.getLong("bytes",0),p.getLong("total",-1),p.getString("message","").orEmpty(),p.getString("name","").orEmpty(),p.getInt("code",0))
 }
 @Synchronized fun cancel(c:Context){
  val p=prefs(c);val id=p.getLong("id",0);if(activeId==id)activeId=0
  uri(c)?.let{runCatching{c.contentResolver.delete(it,null,null)}}
  file(c,id).delete();p.edit().clear().commit()
 }
 @Synchronized fun finished(id:Long){if(activeId==id)activeId=0}
 @Synchronized private fun rememberUri(c:Context,id:Long,uri:Uri):Boolean =
  active(c,id) && prefs(c).edit().putString("uri",uri.toString()).commit()
 fun transfer(c:Context,id:Long,onProgress:(Long,Long)->Unit){
  val p=prefs(c);if(!active(c,id))return
  val local=file(c,id);var published:Uri?=null;var complete=false
  try {
   var savedAt=0L
   ApkTransfer.download(p.getString("url","").orEmpty(),local,{active(c,id)},{}, {bytes,total->
    val now=android.os.SystemClock.elapsedRealtime()
    if(now-savedAt>=500 || bytes==total || bytes==0L){
     check(update(c,id,DownloadManager.STATUS_RUNNING,bytes,total,"Скачиваем обновление…")){"Загрузка отменена"}
     onProgress(bytes,total);savedAt=now
    }
   })
   check(active(c,id)){"Загрузка отменена"}
   check(update(c,id,DownloadManager.STATUS_RUNNING,local.length(),local.length(),"Проверяем APK…"))
   ApkDownloads.validateFile(c,local,p.getInt("code",0))
   check(active(c,id)){"Загрузка отменена"}
   val values=ContentValues().apply{
    put(MediaStore.Downloads.DISPLAY_NAME,"quic-lab-${p.getInt("code",0)}-$id.apk")
    put(MediaStore.Downloads.MIME_TYPE,"application/vnd.android.package-archive")
    put(MediaStore.Downloads.RELATIVE_PATH,Environment.DIRECTORY_DOWNLOADS)
    put(MediaStore.Downloads.IS_PENDING,1)
   }
   val target=requireNotNull(c.contentResolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI,values)){"Не удалось создать APK в «Загрузках»"}
   published=target;check(rememberUri(c,id,target)){"Загрузка отменена"}
   local.inputStream().use{input->requireNotNull(c.contentResolver.openOutputStream(target)).use{out->
    ApkTransfer.copyBody(input,out,local.length(),ApkTransfer.MAX_BYTES,{active(c,id)}){}
   }}
   check(active(c,id)){"Загрузка отменена"}
   check(c.contentResolver.update(target,ContentValues().apply{put(MediaStore.Downloads.IS_PENDING,0)},null,null)==1)
   complete=update(c,id,DownloadManager.STATUS_SUCCESSFUL,local.length(),local.length(),"APK скачан в «Загрузки»")
  }catch(e:Exception){
   update(c,id,DownloadManager.STATUS_FAILED,local.length(),-1,e.message ?: "Не удалось скачать APK. Повторите попытку")
  }finally{
   local.delete()
   if(!complete)published?.let{runCatching{c.contentResolver.delete(it,null,null)}}
   finished(id)
   if(prefs(c).getLong("id",0)==id)ApkDownloads.notifyComplete(c,id)
  }
 }
}

/** Only a user-initiated download starts this short-lived foreground service. */
class ApkTransferService:Service(){
 private var worker:Thread?=null
 private var workerId=0L
 override fun onBind(intent:Intent?)=null
 override fun onStartCommand(intent:Intent?,flags:Int,startId:Int):Int {
  val id=intent?.getLongExtra("id",0) ?: 0
  startForeground(7402,notification(0,-1))
  if(id==0L || !OwnedApkDownloads.active(this,id)){stopSelf(startId);return START_NOT_STICKY}
  if(workerId==id && worker?.isAlive==true)return START_NOT_STICKY
  workerId=id
  worker=Thread({
   try{OwnedApkDownloads.transfer(applicationContext,id){bytes,total->
    if(OwnedApkDownloads.active(this,id))getSystemService(NotificationManager::class.java).notify(7402,notification(bytes,total))
   }}finally{stopSelfResult(startId)}
  },"apk-download").also{it.start()}
  return START_NOT_STICKY
 }
 override fun onDestroy(){OwnedApkDownloads.finished(workerId);super.onDestroy()}
 private fun notification(bytes:Long,total:Long):Notification {
  val nm=getSystemService(NotificationManager::class.java)
  nm.createNotificationChannel(NotificationChannel("apk_transfer","Скачивание обновления",NotificationManager.IMPORTANCE_LOW))
  val pi=PendingIntent.getActivity(this,7402,Intent(this,ApkUpdateActivity::class.java),PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
  return Notification.Builder(this,"apk_transfer").setSmallIcon(android.R.drawable.stat_sys_download)
   .setContentTitle("QUIC Lab · Скачивание обновления").setContentText(if(total>0)"${bytes*100/total}%" else "Подключаемся к серверу…")
   .setProgress(100,if(total>0)(bytes*100/total).toInt() else 0,total<=0).setOngoing(true).setOnlyAlertOnce(true).setContentIntent(pi).build()
 }
}
