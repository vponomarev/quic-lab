package ru.vpnc.quiclab
import android.app.*
import android.content.Intent
import android.net.Uri
import android.os.*
import android.provider.Settings
import android.view.View
import android.widget.*
import java.io.File

/** A visible user gesture is required for install; background completion only notifies. */
class ApkUpdateActivity:Activity(){
 private val handler=Handler(Looper.getMainLooper())
 private lateinit var status:TextView
 private lateinit var progress:ProgressBar
 private lateinit var primary:Button
 private lateinit var cancel:Button
 private var resumed=false
 private var validating=false
 private var autoInstall=false
 private var awaitingPermission=false
 private var error:String?=null
 private var staged:File?=null
 private val tick=object:Runnable{override fun run(){render();handler.postDelayed(this,500)}}
 override fun onCreate(state:Bundle?){
  super.onCreate(state)
  val ui=ClientUi(this);val root=ui.column();val card=ui.card(root)
  card.addView(ui.text("Обновление приложения",23f,true));card.addView(ui.text("Установлено: ${BuildConfig.VERSION_NAME}"))
  status=ui.text("");status.accessibilityLiveRegion=View.ACCESSIBILITY_LIVE_REGION_POLITE;card.addView(status)
  progress=ProgressBar(this,null,android.R.attr.progressBarStyleHorizontal);progress.max=100;ui.add(card,progress)
  primary=ui.button("Установить",true){error=null;val s=ApkDownloads.snapshot(this);if(s.status==DownloadManager.STATUS_SUCCESSFUL)install() else attempt{ApkDownloads.retry(this);autoInstall=true}};ui.add(card,primary)
  cancel=ui.button("Отменить скачивание"){attempt{if(ApkDownloads.snapshot(this).status==DownloadManager.STATUS_SUCCESSFUL){ApkDownloads.retry(this);autoInstall=true}else{ApkDownloads.cancel(this);autoInstall=false};error=null;render()}};ui.add(card,cancel)
  ui.add(card,ui.button("Назад"){finish()});ui.install(root)
  if(state==null && Build.VERSION.SDK_INT>=33 && checkSelfPermission(android.Manifest.permission.POST_NOTIFICATIONS)!=android.content.pm.PackageManager.PERMISSION_GRANTED && !getPreferences(MODE_PRIVATE).getBoolean("notificationAsked",false)){
   getPreferences(MODE_PRIVATE).edit().putBoolean("notificationAsked",true).apply()
   requestPermissions(arrayOf(android.Manifest.permission.POST_NOTIFICATIONS),7401)
  }
  awaitingPermission=state?.getBoolean("permission") ?: false
  autoInstall=state?.getBoolean("auto") ?: intent.getBooleanExtra("install",false)
  if(state==null && intent.hasExtra("url"))attempt{
   ApkDownloads.start(this,ApplicationUpdates.Offer("","",intent.getStringExtra("name").orEmpty(),intent.getIntExtra("code",0),intent.getStringExtra("url").orEmpty()));autoInstall=true
  }
 }
 override fun onSaveInstanceState(out:Bundle){out.putBoolean("auto",autoInstall || validating);out.putBoolean("permission",awaitingPermission);super.onSaveInstanceState(out)}
 override fun onResume(){super.onResume();resumed=true;ApkDownloads.dismissNotification(this)
  if(awaitingPermission){awaitingPermission=false;autoInstall=packageManager.canRequestPackageInstalls();if(!autoInstall)error="Установка не разрешена. APK сохранён; разрешение можно выдать при следующей попытке."}
  handler.post(tick)
 }
 override fun onPause(){resumed=false;handler.removeCallbacks(tick);super.onPause()}
 private fun attempt(block:()->Unit){try{block()}catch(e:Exception){error=e.message ?: "Не удалось выполнить действие";autoInstall=false}}
 private fun render(){
  val s=runCatching{ApkDownloads.snapshot(this)}.getOrElse{error=it.message;ApkDownloads.State()}
  val amount=if(s.bytes>0)"\n%.1f МБ".format(s.bytes/1048576.0)+(if(s.total>0)" / %.1f МБ".format(s.total/1048576.0) else "") else ""
  val text=if(validating)"Проверяем APK…" else error ?: (s.name+"\n"+s.message+amount)
  val note=if(!getSystemService(NotificationManager::class.java).areNotificationsEnabled())"\nУведомления выключены. После скачивания вернитесь на этот экран для установки." else ""
  if(status.text.toString()!=text+note)status.text=text+note
  progress.visibility=if(s.active || validating)View.VISIBLE else View.GONE
  progress.isIndeterminate=validating || s.total<=0
  if(s.total>0)progress.progress=(s.bytes*100/s.total).coerceIn(0,100).toInt()
  primary.visibility=if(!s.active && s.id>0)View.VISIBLE else View.GONE;primary.isEnabled=!validating
  primary.text=if(s.status==DownloadManager.STATUS_SUCCESSFUL)"Установить скачанное" else "Повторить скачивание"
  cancel.visibility=if(s.active || s.status==DownloadManager.STATUS_SUCCESSFUL)View.VISIBLE else View.GONE
  cancel.isEnabled=!validating;cancel.text=if(s.active)"Отменить скачивание" else "Скачать заново"
  if(autoInstall && resumed && !validating && s.status==DownloadManager.STATUS_SUCCESSFUL){autoInstall=false;install()}
 }
 private fun install(){
  if(validating)return
  if(!packageManager.canRequestPackageInstalls()){
   awaitingPermission=true
   attempt{startActivity(Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,Uri.parse("package:$packageName")))}
   return
  }
  validating=true;error=null;render()
  val c=applicationContext
  Thread({
   val result=runCatching{ApkDownloads.prepareInstaller(c)}
   handler.post{
    validating=false
    if(isDestroyed){result.getOrNull()?.delete();return@post}
    result.onSuccess{file->staged?.delete();staged=file
     if(resumed)attempt{startActivity(ApkDownloads.installerIntent(this,file))} else {autoInstall=true;ApkDownloads.notifyComplete(this,ApkDownloads.snapshot(this).id)}
    }.onFailure{error=it.message ?: "Не удалось проверить APK"}
    if(resumed)render()
   }
  },"apk-validation").start()
 }
}
