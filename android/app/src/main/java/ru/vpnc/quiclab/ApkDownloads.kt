package ru.vpnc.quiclab

import android.app.*
import android.content.*
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Environment
import androidx.core.content.FileProvider
import org.json.JSONObject
import java.io.File
import java.security.MessageDigest
import java.util.UUID

/** DownloadManager owns transfer/retries across Activity and process lifetimes. */
internal object ApkDownloads {
 const val NOTIFICATION=7401
 private const val MAX=268435456L
 private fun prefs(c:Context)=c.getSharedPreferences("apk_download",Context.MODE_PRIVATE)
 private fun manager(c:Context)=c.getSystemService(DownloadManager::class.java)
 data class State(val id:Long=0,val status:Int=0,val bytes:Long=0,val total:Long=-1,val message:String="",val name:String="",val code:Int=0) {
  val active get()=status in listOf(DownloadManager.STATUS_PENDING,DownloadManager.STATUS_RUNNING,DownloadManager.STATUS_PAUSED)
 }
 fun hasDownload(c:Context)=prefs(c).getLong("id",0)>0
 @Synchronized fun start(c:Context,offer:ApplicationUpdates.Offer) {
  require(offer.code>=BuildConfig.VERSION_CODE){"Предложенная версия старее установленной"}
  ServiceTransfer.validateMetadata(JSONObject().put("apk_url",offer.url))
  val previous=snapshot(c)
  if(previous.id>0 && prefs(c).getString("url","")==offer.url && previous.code==offer.code && (previous.active || previous.status==DownloadManager.STATUS_SUCCESSFUL))return
  check(!previous.active){"Уже скачивается другое обновление"}
  val request=DownloadManager.Request(Uri.parse(offer.url))
   .setTitle("QUIC Lab · ${offer.name}").setDescription("Обновление приложения")
   .setMimeType("application/vnd.android.package-archive")
   .setNotificationVisibility(DownloadManager.Request.VISIBILITY_VISIBLE)
   .setDestinationInExternalPublicDir(Environment.DIRECTORY_DOWNLOADS,"quic-lab-${offer.code}-${UUID.randomUUID().toString().take(8)}.apk")
  val id=manager(c).enqueue(request)
  if(!prefs(c).edit().putLong("id",id).putString("url",offer.url).putString("name",offer.name).putInt("code",offer.code).commit()){
   manager(c).remove(id);error("Не удалось сохранить состояние загрузки")
  }
 }
 fun retry(c:Context){val p=prefs(c);val o=ApplicationUpdates.Offer("","",p.getString("name","").orEmpty(),p.getInt("code",0),p.getString("url","").orEmpty());cancel(c);start(c,o)}
 @Synchronized fun cancel(c:Context){val id=prefs(c).getLong("id",0);if(id>0)manager(c).remove(id);prefs(c).edit().remove("id").commit();dismissNotification(c)}
 fun snapshot(c:Context):State {
  val p=prefs(c);val id=p.getLong("id",0);if(id==0L)return State(message="Загрузка отменена")
  val name=p.getString("name","").orEmpty();val code=p.getInt("code",0)
  manager(c).query(DownloadManager.Query().setFilterById(id)).use {q->
   if(q==null || !q.moveToFirst())return State(id,DownloadManager.STATUS_FAILED,message="Файл удалён или загрузка недоступна",name=name,code=code)
   fun number(key:String)=q.getLong(q.getColumnIndexOrThrow(key))
   val status=number(DownloadManager.COLUMN_STATUS).toInt();val reason=number(DownloadManager.COLUMN_REASON).toInt()
   val message=when(status){
    DownloadManager.STATUS_PENDING->"Ожидаем начало скачивания…"
    DownloadManager.STATUS_RUNNING->"Скачиваем обновление…"
    DownloadManager.STATUS_PAUSED->when(reason){DownloadManager.PAUSED_WAITING_FOR_NETWORK->"Ожидаем сеть";DownloadManager.PAUSED_QUEUED_FOR_WIFI->"Ожидаем Wi-Fi";else->"Загрузка приостановлена, будет повторена"}
    DownloadManager.STATUS_SUCCESSFUL->"APK скачан в «Загрузки»"
    else->when(reason){DownloadManager.ERROR_INSUFFICIENT_SPACE->"Недостаточно места";in 400..599->"Сервер ответил HTTP $reason";else->"Не удалось скачать APK (код $reason)"}
   }
   return State(id,status,number(DownloadManager.COLUMN_BYTES_DOWNLOADED_SO_FAR),number(DownloadManager.COLUMN_TOTAL_SIZE_BYTES),message,name,code)
  }
 }
 fun validateIdentity(pkg:String,version:Long,signers:Set<String>,installedPkg:String,installedVersion:Long,offeredVersion:Long,installedSigners:Set<String>){
  require(pkg==installedPkg){"Это APK другого приложения"}
  require(signers.isNotEmpty() && signers==installedSigners){"Подпись APK не совпадает с установленным приложением"}
  require(version>=installedVersion && version==offeredVersion){"Версия скачанного APK не соответствует обновлению. Повторите проверку версий"}
 }
 @Suppress("DEPRECATION") fun validateFile(c:Context,file:File,code:Int){
  require(file.length() in 1..MAX){"Некорректный размер APK"}
  val pm=c.packageManager;val flags=PackageManager.GET_SIGNING_CERTIFICATES
  val archive=requireNotNull(pm.getPackageArchiveInfo(file.absolutePath,flags)){"Скачанный файл не является APK"}
  val installed=pm.getPackageInfo(c.packageName,flags)
  fun signers(p:android.content.pm.PackageInfo)=p.signingInfo?.apkContentsSigners?.map{MessageDigest.getInstance("SHA-256").digest(it.toByteArray()).joinToString(""){b->"%02x".format(b.toInt() and 255)}}?.toSet().orEmpty()
  validateIdentity(archive.packageName,archive.longVersionCode,signers(archive),c.packageName,installed.longVersionCode,code.toLong(),signers(installed))
 }
 // Validate an immutable private copy, not a public file another app can replace.
 fun prepareInstaller(c:Context):File {
  val state=snapshot(c);check(state.status==DownloadManager.STATUS_SUCCESSFUL){state.message}
  val dir=File(c.cacheDir,"updates");check(dir.mkdirs() || dir.isDirectory)
  dir.listFiles()?.filter{it.name.startsWith("download-") && it.lastModified()<System.currentTimeMillis()-86400000L}?.forEach{it.delete()}
  val file=File(dir,"download-${state.id}-${UUID.randomUUID()}.apk")
  try {
   val uri=requireNotNull(manager(c).getUriForDownloadedFile(state.id)){"APK больше не доступен"}
   requireNotNull(c.contentResolver.openInputStream(uri)).use {input->file.outputStream().use {out->
    val b=ByteArray(65536);var count=0L
    while(true){val n=input.read(b);if(n<0)break;count+=n;require(count<=MAX){"APK превышает допустимый размер"};out.write(b,0,n)}
   }}
   validateFile(c,file,state.code)
   check(prefs(c).getLong("id",0)==state.id){"Загрузка изменилась, повторите установку"}
   return file
  }catch(e:Exception){file.delete();throw e}
 }
 fun installerIntent(c:Context,file:File)=Intent(Intent.ACTION_VIEW)
  .setDataAndType(FileProvider.getUriForFile(c,c.packageName+".diagnostics",file),"application/vnd.android.package-archive")
  .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
 fun dismissNotification(c:Context){c.getSystemService(NotificationManager::class.java).cancel(NOTIFICATION)}
 fun notifyComplete(c:Context,id:Long){
  val state=snapshot(c);if(state.id!=id || state.active || state.status==0)return
  val nm=c.getSystemService(NotificationManager::class.java)
  nm.createNotificationChannel(NotificationChannel("apk_updates","Обновления приложения",NotificationManager.IMPORTANCE_DEFAULT))
  val intent=Intent(c,ApkUpdateActivity::class.java).putExtra("install",state.status==DownloadManager.STATUS_SUCCESSFUL)
  val pending=PendingIntent.getActivity(c,NOTIFICATION,intent,PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
  val n=Notification.Builder(c,"apk_updates").setSmallIcon(android.R.drawable.stat_sys_download_done)
   .setContentTitle(if(state.status==DownloadManager.STATUS_SUCCESSFUL)"Обновление скачано · Установить" else "Ошибка скачивания обновления")
   .setContentText("QUIC Lab ${state.name}: ${state.message}").setContentIntent(pending).setAutoCancel(true).build()
  runCatching{nm.notify(NOTIFICATION,n)}
 }
}

class ApkDownloadReceiver:BroadcastReceiver(){
 override fun onReceive(c:Context,intent:Intent){
  if(intent.action!=DownloadManager.ACTION_DOWNLOAD_COMPLETE)return
  val id=intent.getLongExtra(DownloadManager.EXTRA_DOWNLOAD_ID,-1)
  val pending=goAsync()
  Thread({try{runCatching{ApkDownloads.notifyComplete(c,id)}}finally{pending.finish()}},"apk-complete").start()
 }
}
