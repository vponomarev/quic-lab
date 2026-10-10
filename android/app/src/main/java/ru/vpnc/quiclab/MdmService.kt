package ru.vpnc.quiclab

import android.app.*
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import java.io.Closeable
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.random.Random

/** Lifecycle owner must check durable active state before returning a session.
 * Its pause persists revocation before returning. Wired by Task 3. */
internal interface MdmSession:Closeable { fun sync();fun pause();fun isActive():Boolean=true }

class MdmService:Service(){
 private var session:MdmSession?=null
 private var worker:Thread?=null
 private val ending=AtomicBoolean(false)
 private var retained=false
 private val handler=Handler(Looper.getMainLooper())
 private val checkpoint=object:Runnable{
  override fun run(){
   if(ending.get())return
   runCatching{VpnBudgetRun.sharedForContext(this@MdmService).checkpoint()}
    .onFailure{status="Не удалось сохранить LTE-бюджет";session?.close();stopSelf()}
   if(!ending.get())handler.postDelayed(this,2000)
  }
 }
 override fun onBind(intent:Intent?):IBinder?=null
 override fun onStartCommand(intent:Intent?,flags:Int,startId:Int):Int{
  if(intent?.action=="pause"){
   try{val current=session;if(current!=null)current.pause() else MdmRuntime.controller(this).pause()}catch(_:Exception){status="Не удалось сохранить паузу MDM"}finally{stopSelf()}
   return START_NOT_STICKY
  }
  if(session!=null)return START_STICKY
  val selected=try{val factory=owner;if(factory!=null)factory(applicationContext) else MdmRuntime.session(applicationContext)}
   catch(_:Exception){status="Хранилище MDM недоступно";stopSelf();return START_NOT_STICKY}
  if(selected==null){stopSelf();return START_NOT_STICKY}
  session=selected
  try{
   AppServiceNotification.start(this,"mdm",if(Build.VERSION.SDK_INT>=34)ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE else 0)
   VpnBudgetRun.sharedForContext(this).acquireControl(VpnBudgetSettings.limitBytes(this));retained=true
   // Retry only once management has a legitimate foreground service.
   VpnProcessRecovery.restore(this)
   runCatching{MdmTelemetry.reconcile(this)}
   status="MDM включено"
   handler.post(checkpoint)
   worker=Thread({
    var delay=1000L
    while(!ending.get()){
     try{if(!selected.isActive()){stopSelf();break};selected.sync();delay=1000;status="MDM подключено"}
     catch(_:InterruptedException){stopSelf();break}
     catch(_:Exception){
      if(ending.get())break
      status="MDM: ожидание связи"
      try{Thread.sleep(delay+Random.nextLong(0,delay/4+1))}catch(_:InterruptedException){break}
      delay=(delay*2).coerceAtMost(60000)
     }
    }
   },"mdm-control").also{it.start()}
  }catch(_:Exception){
   status="Android не разрешил запуск MDM или недоступен LTE-бюджет"
   stopSelf();return START_NOT_STICKY
  }
  return START_STICKY
 }
 override fun onDestroy(){
  ending.set(true);handler.removeCallbacks(checkpoint);session?.close();worker?.interrupt()
  if(retained){retained=false;runCatching{VpnBudgetRun.sharedForContext(this).releaseControl()}}
  session=null;AppServiceNotification.stop(this);super.onDestroy()
 }
 companion object {
  @Volatile internal var owner:((Context)->MdmSession?)?=null
  @Volatile internal var status="MDM выключено"
  fun start(context:Context){
   status="MDM запускается…"
   try{context.startForegroundService(Intent(context,MdmService::class.java))}
   catch(e:RuntimeException){status="Android запретил фоновый запуск MDM";throw e}
  }
  fun stop(context:Context){context.stopService(Intent(context,MdmService::class.java))}
 }
}
