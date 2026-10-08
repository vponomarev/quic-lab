package ru.vpnc.quiclab

import android.Manifest
import android.app.Activity
import android.content.pm.PackageManager
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.*
import java.text.DateFormat
import java.util.Date

class MdmTelemetryActivity:Activity(){
 private lateinit var ui:ClientUi
 private lateinit var status:TextView
 private lateinit var consent:CheckBox
 private lateinit var permissions:TextView
 private val handler=Handler(Looper.getMainLooper())
 private val tick=object:Runnable{
  override fun run(){
   refreshPermissions()
   val last=MdmTelemetry.lastUpload(this@MdmTelemetryActivity)
   status.text=MdmTelemetry.status+"\nПоследняя отправка: "+(if(last==0L)"ещё не было" else DateFormat.getDateTimeInstance().format(Date(last)))+"\nУдалено из очереди по сроку/лимиту: "+runCatching{MdmTelemetryStore(this@MdmTelemetryActivity).dropped()}.getOrDefault(0)
   handler.postDelayed(this,1000)
  }
 }
 override fun onCreate(savedInstanceState:Bundle?){
  super.onCreate(savedInstanceState);ui=ClientUi(this)
  val root=ui.column();root.addView(ui.button("Назад"){finish()})
  val panel=ui.column().apply{setPadding(ui.dp(16),ui.dp(8),ui.dp(16),ui.dp(16))}
  root.addView(ScrollView(this).apply{addView(panel)},LinearLayout.LayoutParams(-1,0,1f));ui.install(root)
  panel.addView(ui.text("Данные Wi-Fi и сот",22f,true))
  val state=MdmStore(this).read()
  if(state.binding==null){panel.addView(ui.text("Сначала подключите отдельную привязку MDM в разделе «Управление устройством»."));return}
  status=ui.text("");panel.addView(status);handler.post(tick)
  panel.addView(ui.text("Данные Wi-Fi и сот позволяют определить местоположение. GPS-координаты не собираются."))
  val controls=LinearLayout(this).apply{orientation=LinearLayout.HORIZONTAL}
  consent=CheckBox(this).apply{
   text="Разрешить отправку";isChecked=state.rights.telemetry&&state.rights.geo
   setOnCheckedChangeListener{_,checked->
    val controller=MdmRuntime.controller(this@MdmTelemetryActivity)
    runCatching{controller.setRights(controller.read().rights.copy(telemetry=checked,geo=checked))}
     .onFailure{Toast.makeText(this@MdmTelemetryActivity,"Не удалось сохранить согласие",Toast.LENGTH_LONG).show()}
    if(checked)ensurePermissions()
   }
  }
  controls.addView(consent,LinearLayout.LayoutParams(0,-2,1f))
  controls.addView(CheckBox(this).apply{
   text="Только Wi-Fi";isChecked=MdmTelemetry.wifiOnly(this@MdmTelemetryActivity)
   setOnCheckedChangeListener{_,v->MdmTelemetry.setWifiOnly(this@MdmTelemetryActivity,v)}
  })
  panel.addView(controls)
  panel.addView(ui.button("Начать сбор и отправку",true){
   if(!MdmTelemetryPolicy.consented(MdmStore(this).read())){
    Toast.makeText(this,"Включите MDM и разрешите отправку данных",Toast.LENGTH_LONG).show()
   }else if(ensurePermissions()){
    runCatching{MdmTelemetry.setManual(this,true);MdmTelemetryService.start(this)}
     .onFailure{MdmTelemetry.status="Не удалось запустить сбор · проверьте разрешения Android"}
   }
  })
  panel.addView(ui.button("Остановить ручной сбор"){
   MdmTelemetry.setManual(this,false)
   if(!MdmTelemetry.serverEnabled(this))MdmTelemetryService.stop(this)
   MdmTelemetry.status=if(MdmTelemetry.wanted(this))"Продолжается сбор по запросу сервера" else "Ручной сбор остановлен"
  })
  panel.addView(ui.text("Без галочки отправка разрешена и через LTE. «Только Wi-Fi» накапливает данные и отправляет пакетом через любой Wi-Fi. Очередь: до 24 часов / 50 МиБ."))
  panel.addView(ui.text("SSID/BSSID точки доступа, MCC/MNC, идентификаторы обслуживающей соты и уровень сигнала позволяют определить ваше местоположение. GPS-координаты не собираются. Полученные сервером данные сохраняются после отключения."))
  panel.addView(ui.button("Разрешения Android · сбор в фоне"){
   startActivity(android.content.Intent(android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS,android.net.Uri.parse("package:"+packageName)))
  })
  permissions=ui.text("");panel.addView(permissions);refreshPermissions()
  panel.addView(ui.text("Сбор сохраняется до вашей остановки, в том числе после обновления приложения. Для восстановления в фоне разрешите геопозицию «Всегда». При разрешении «При использовании» достаточно открыть приложение. GPS не запрашивается."))
  panel.addView(ui.text("Снятие разрешения полностью прекращает сбор и удаляет неотправленные данные. Пауза MDM также останавливает отправку."))
 }
 override fun onResume(){
  super.onResume()
  refreshPermissions()
  runCatching{MdmTelemetry.reconcile(this,foreground=true)}
 }
 private fun refreshPermissions(){
  if(!::permissions.isInitialized)return
  fun granted(p:String)=checkSelfPermission(p)==PackageManager.PERMISSION_GRANTED
  fun line(ok:Boolean,text:String)=(if(ok)"☑ " else "☐ ")+text
  permissions.text=listOf(
   line(granted(Manifest.permission.ACCESS_FINE_LOCATION),"Точная геопозиция — для данных Wi-Fi и сот"),
   line(granted(Manifest.permission.ACCESS_BACKGROUND_LOCATION),"Геопозиция «Всегда» — для восстановления в фоне"),
   line(getSystemService(android.location.LocationManager::class.java).isLocationEnabled,"Геолокация Android включена")
  ).joinToString("\n")
 }
 private fun ensurePermissions():Boolean{
  if(checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION)==PackageManager.PERMISSION_GRANTED)return true
  requestPermissions(arrayOf(Manifest.permission.ACCESS_FINE_LOCATION,Manifest.permission.ACCESS_COARSE_LOCATION),724)
  return false
 }
 override fun onDestroy(){handler.removeCallbacksAndMessages(null);super.onDestroy()}
}
