package ru.vpnc.quiclab

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.*

class TemporaryEditorActivity:Activity(){
 private lateinit var ui:ClientUi
 private lateinit var panel:LinearLayout
 private lateinit var status:TextView
 private val handler=Handler(Looper.getMainLooper())
 private var invitation:TemporaryEditorInvitation?=null
 private val tick=object:Runnable{override fun run(){
  val s=TemporaryEditorRuntime.current
  status.text=TemporaryEditorRuntime.status+if(s!=null)"\nОсталось ${s.lease.remainingSeconds()/60} мин ${s.lease.remainingSeconds()%60} с" else ""
  handler.postDelayed(this,1000)
 }}
 override fun onCreate(savedInstanceState:Bundle?){
  super.onCreate(savedInstanceState);ui=ClientUi(this)
  // Pair token is not saved to state/preferences or retained in this activity's Intent.
  val supplied=intent.dataString?:intent.getStringExtra("editor_invitation")
  intent.data=null;intent.removeExtra("editor_invitation")
  invitation=runCatching{supplied?.let{TemporaryEditorInvitation.parse(it)}}.getOrNull()
  val root=ui.column();root.addView(ui.button("Назад"){finish()});panel=ui.column()
  root.addView(ScrollView(this).apply{addView(panel)},LinearLayout.LayoutParams(-1,0,1f));ui.install(root);render()
 }
 private fun render(){
  panel.removeAllViews();panel.addView(ui.text("Временный WEB-редактор",22f,true));status=ui.text(TemporaryEditorRuntime.status);panel.addView(status)
  if(TemporaryEditorRuntime.current!=null){
   panel.addView(ui.text("Сервер: ${TemporaryEditorRuntime.current!!.invitation.endpoint}"))
   panel.addView(ui.button("Завершить доступ",true){TemporaryEditorRuntime.stop();stopService(Intent(this,TemporaryEditorService::class.java));invitation=null;render()});return
  }
  if(MdmConfiguration.managed(this)){
   panel.addView(ui.text("Настройками управляет MDM. Используйте редактор устройства в админке вашего MDM-сервера: изменения будут сохранены на сервере и доставлены через MDM. Временная сессия не может обойти это управление."))
   panel.addView(ui.button("Управление устройством (MDM)"){startActivity(Intent(this,MdmEnrollActivity::class.java))});return
  }
  val proposed=invitation
  if(proposed==null){
   panel.addView(ui.text("В админке сервера откройте «WEB-редактор» и создайте сессию. Затем отсканируйте QR или вставьте ссылку."))
   panel.addView(ui.button("Сканировать QR"){startActivity(Intent(this,ProfileScanActivity::class.java).putExtra("editor_only",true))})
   val link=EditText(this).apply{hint="quiclab://editor/pair#…";setSaveEnabled(false)};panel.addView(link)
   panel.addView(ui.button("Проверить ссылку"){try{invitation=TemporaryEditorInvitation.parse(link.text.toString().trim());render()}catch(_:Exception){status.text="Неверная ссылка редактора"}});return
  }
  panel.addView(ui.text("Сервер: ${proposed.endpoint}\nСрок доступа: ${proposed.minutes} минут",18f,true))
  panel.addView(ui.text("Сервер получит текущие настройки и сможет изменить подключения, DNS, лимиты, диагностику и выбор приложений. Существующие ключи VPN не отправляются. Применение может переподключить работающий VPN. Изменения сохранятся после завершения доступа. MDM, геолокация и команды включения VPN не разрешаются. Доступ можно завершить здесь или из уведомления; после закрытия процесса он не возобновится."))
  panel.addView(ui.button("Разрешить редактирование",true){try{TemporaryEditorRuntime.start(this,proposed);invitation=null;render()}catch(_:Exception){status.text="Не удалось открыть редактор. Проверьте права и настройки MDM."}})
  panel.addView(ui.button("Отмена"){invitation=null;render()})
 }
 override fun onResume(){super.onResume();handler.post(tick)}
 override fun onPause(){handler.removeCallbacks(tick);super.onPause()}
}
