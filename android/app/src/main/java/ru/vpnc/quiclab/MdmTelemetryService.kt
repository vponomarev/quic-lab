package ru.vpnc.quiclab

import android.app.*
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.IBinder
import java.util.concurrent.atomic.AtomicBoolean

/** Explicit location FGS keeps radio reads legal while the screen is off. */
class MdmTelemetryService:Service(){
 private val ending=AtomicBoolean(false)
 private var worker:Thread?=null
 override fun onBind(intent:Intent?):IBinder?=null
 override fun onStartCommand(intent:Intent?,flags:Int,startId:Int):Int{
  if(intent?.action=="stop"){
   MdmTelemetry.manual=false
   // Notification stop revokes radio consent, even if the server requests collection.
   runCatching{
    val controller=MdmRuntime.controller(this)
    controller.setRights(controller.read().rights.copy(geo=false))
   }
   MdmTelemetry.stop(this,true);stopSelf();return START_NOT_STICKY
  }
  if(!MdmTelemetry.wanted(this)){stopSelf();return START_NOT_STICKY}
  if(worker!=null)return START_NOT_STICKY
  try{
   AppServiceNotification.start(this,"geo",ServiceInfo.FOREGROUND_SERVICE_TYPE_LOCATION)
  }catch(_:Exception){
   MdmTelemetry.status="Android не разрешил сбор: откройте экран телеметрии и проверьте разрешение геопозиции"
   stopSelf();return START_NOT_STICKY
  }
  running=true
  worker=Thread({
   while(!ending.get()){
    try{
     if(!MdmTelemetry.wanted(this)){stopSelf();break}
     val generation=MdmStore(this).read().localGeneration
     MdmTelemetry.status="Сбор данных Wi-Fi и сот"
     val sample=MdmRadioSampler(this).sample()
     MdmTelemetry.append(this,generation,sample)
     try{MdmTelemetry.upload(this)}
     catch(_:Exception){MdmTelemetry.status=if(MdmTelemetry.wifiOnly(this))"Данные сохранены · ожидание Wi-Fi/сервера" else "Данные сохранены · ожидание связи с сервером"}
     Thread.sleep(30000)
    }catch(_:InterruptedException){break}
     catch(_:Exception){MdmTelemetry.status="Ошибка сбора телеметрии";try{Thread.sleep(30000)}catch(_:InterruptedException){break}}
   }
  },"mdm-radio").also{it.start()}
  return START_NOT_STICKY
 }
 override fun onDestroy(){ending.set(true);worker?.interrupt();running=false;AppServiceNotification.stop(this);super.onDestroy()}
 companion object{
  @Volatile var running=false
  fun start(c:Context){c.startForegroundService(Intent(c,MdmTelemetryService::class.java))}
  fun stop(c:Context){c.stopService(Intent(c,MdmTelemetryService::class.java))}
 }
}
