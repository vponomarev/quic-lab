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
 private val handler=Handler(Looper.getMainLooper())
 private val tick=object:Runnable{
  override fun run(){
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
  panel.addView(ui.text("SSID/BSSID точки доступа, MCC/MNC, идентификаторы обслуживающей соты и уровень сигнала позволяют определить ваше местоположение. GPS-координаты не собираются. Полученные сервером данные сохраняются после отключения."))
  val state=MdmStore(this).read()
  if(state.binding==null){panel.addView(ui.text("Сначала подключите отдельную привязку MDM в разделе «Управление устройством»."));return}
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
  panel.addView(ui.text("Без галочки отправка разрешена и через LTE. «Только Wi-Fi» накапливает данные и отправляет пакетом через любой Wi-Fi. Очередь: до 24 часов / 50 МиБ."))
  panel.addView(ui.button("Начать сбор и отправку",true){
   if(!MdmTelemetryPolicy.consented(MdmStore(this).read())){
    Toast.makeText(this,"Включите MDM и разрешите отправку данных",Toast.LENGTH_LONG).show()
   }else if(ensurePermissions()){
    MdmTelemetry.manual=true
    runCatching{MdmTelemetryService.start(this)}.onFailure{MdmTelemetry.manual=false;MdmTelemetry.status="Не удалось запустить сбор"}
   }
  })
  panel.addView(ui.button("Остановить ручной сбор"){
   MdmTelemetry.manual=false
   if(!MdmTelemetry.serverEnabled(this))MdmTelemetryService.stop(this)
   MdmTelemetry.status=if(MdmTelemetry.wanted(this))"Продолжается сбор по запросу сервера" else "Ручной сбор остановлен"
  })
  panel.addView(ui.button("Разрешения Android · сбор в фоне"){
   startActivity(android.content.Intent(android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS,android.net.Uri.parse("package:"+packageName)))
  })
  panel.addView(ui.text("Для запуска по запросу сервера без открытия приложения разрешите геопозицию «Всегда» в настройках Android. Для ручного сбора достаточно «При использовании». GPS не запрашивается."))
  panel.addView(ui.text("Снятие разрешения полностью прекращает сбор и удаляет неотправленные данные. Пауза MDM также останавливает отправку."))
  status=ui.text("");panel.addView(status);handler.post(tick)
 }
 override fun onResume(){
  super.onResume()
  if(MdmTelemetry.wanted(this) && !MdmTelemetryService.running && checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION)==PackageManager.PERMISSION_GRANTED){
   runCatching{MdmTelemetryService.start(this)}
  }
 }
 private fun ensurePermissions():Boolean{
  if(checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION)==PackageManager.PERMISSION_GRANTED)return true
  requestPermissions(arrayOf(Manifest.permission.ACCESS_FINE_LOCATION,Manifest.permission.ACCESS_COARSE_LOCATION),724)
  return false
 }
 override fun onDestroy(){handler.removeCallbacksAndMessages(null);super.onDestroy()}
}
