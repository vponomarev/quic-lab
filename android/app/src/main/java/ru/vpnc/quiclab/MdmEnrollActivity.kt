package ru.vpnc.quiclab

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.os.Bundle
import android.widget.*
import java.net.URI

/** Opening an invitation is read-only; only the explicit confirmation enrolls. */
class MdmEnrollActivity:Activity(){
 private lateinit var ui:ClientUi
 private lateinit var panel:LinearLayout
 private lateinit var status:TextView
 private val statusHandler=android.os.Handler(android.os.Looper.getMainLooper())
 private val statusTick=object:Runnable{
  override fun run(){
   if(::status.isInitialized && !busy && status.text.startsWith("MDM")){
    runCatching{MdmRuntime.controller(this@MdmEnrollActivity).read()}.getOrNull()?.let{
     if(it.active)status.text=MdmService.status
    }
   }
   statusHandler.postDelayed(this,1000)
  }
 }
 override fun onResume(){super.onResume();MdmRuntime.restore(this,foreground=true);statusHandler.post(statusTick)}
 override fun onPause(){statusHandler.removeCallbacks(statusTick);super.onPause()}
 private var busy=false
 private var invitation:MdmInvitation?=null
 private val checks=mutableMapOf<MdmRight,CheckBox>()
 private lateinit var lan:Spinner
 override fun onCreate(savedInstanceState:Bundle?){
  super.onCreate(savedInstanceState)
  ui=ClientUi(this)
  val root=ui.column()
  root.addView(ui.button("Назад"){finish()})
  panel=ui.column().apply{setPadding(ui.dp(16),ui.dp(8),ui.dp(16),ui.dp(16))}
  root.addView(ScrollView(this).apply{addView(panel)},LinearLayout.LayoutParams(-1,0,1f))
  ui.install(root)
  val supplied=intent.dataString?:intent.getStringExtra("mdm_invitation")
  try{if(supplied!=null)invitation=MdmInvitation.parse(supplied);render()}
  catch(_:Exception){panel.addView(ui.text("Не удалось открыть MDM. Проверьте приглашение или состояние хранилища."))}
 }
 private fun render(){
  panel.removeAllViews();checks.clear()
  val controller=MdmRuntime.controller(this)
  val state=controller.read()
  panel.addView(ui.text("Управление устройством",22f,true))
  status=ui.text(if(state.active)MdmService.status else if(state.binding!=null)"MDM приостановлено" else "MDM не подключено")
  panel.addView(status)
  if(state.binding!=null){
   panel.addView(ui.text("Сервер: "+URI(state.binding.endpoint).authority))
   panel.addView(ui.button(if(state.active)"Приостановить MDM" else "Возобновить MDM",true){
    work{
     if(controller.read().active){controller.pause();MdmService.stop(this)}
     else{controller.resume();MdmService.start(this)}
    }
   })
   panel.addView(ui.text("Пауза полностью отключает управление. Привязка сохранится; вы сможете возобновить её."))
   panel.addView(ui.button("Данные Wi-Fi и сот"){startActivity(Intent(this,MdmTelemetryActivity::class.java))})
   rights(state.rights)
   panel.addView(ui.button("Сохранить права"){val selected=selectedRights();work{controller.setRights(selected)}})
   panel.addView(ui.button("Удалить MDM"){
    AlertDialog.Builder(this).setTitle("Удалить привязку MDM?")
     .setMessage("Ключ управления и привязка к серверу будут удалены с телефона. Для повторного подключения понадобится приглашение.")
     .setNegativeButton("Отмена",null).setPositiveButton("Удалить"){_,_->work{controller.delete();MdmService.stop(this);invitation=null}}.show()
   })
   return
  }
  if(state.pendingEnrollment){
   panel.addView(ui.text("Регистрация не завершена. Повторите то же приглашение: новая регистрация создана не будет."))
   panel.addView(ui.button("Удалить незавершённую привязку"){work{controller.delete()}})
  }
  val proposed=invitation
  if(proposed==null){
   val link=EditText(this).apply{hint="Отдельная ссылка-приглашение MDM";inputType=android.text.InputType.TYPE_CLASS_TEXT or android.text.InputType.TYPE_TEXT_FLAG_MULTI_LINE}
   panel.addView(link)
   panel.addView(ui.button("Проверить приглашение"){
    try{invitation=MdmInvitation.parse(link.text.toString().trim());render()}
    catch(_:Exception){status.text="Неверное приглашение MDM. VPN-профиль не включает управление устройством."}
   })
   return
  }
  panel.addView(ui.text("Подключение к "+URI(proposed.endpoint).authority,18f,true))
  panel.addView(ui.text("Вы разрешаете выбранные ниже действия. Сервер сможет управлять приложением, даже если VPN выключен. Управление можно в любой момент приостановить или удалить."))
  panel.addView(ui.text(if(proposed.mode=="external")
   "Внешняя конфигурация: на время управления используется конфигурация сервера. При паузе она удаляется, возвращаются личные настройки."
   else "Текущая конфигурация: изменения сервера сохранятся на телефоне после паузы или удаления MDM."))
  panel.addView(ui.text("Сервер определяет режим конфигурации. Внешний режим может быть позже заменён текущим; тогда управляемые настройки сохранятся как личные."))
  val requested=proposed.rights
  panel.addView(ui.text("Запрошены: "+listOfNotNull(
   if(requested.config)"настройки" else null,if(requested.vpn)"включение/выключение VPN" else null,
   if(requested.telemetry)"телеметрия" else null,if(requested.geo)"данные Wi-Fi/сот" else null,
   if(requested.coordinates)"координаты" else null,if(requested.lanMode!="deny")"доступ к локальной сети" else null
  ).joinToString().ifEmpty{"нет прав"}))
  rights(MdmRights())
  panel.addView(ui.button("Подтверждаю: подключить MDM",true){
   val selected=selectedRights()
   work{controller.enroll(proposed,selected);MdmService.start(this)}
  })
 }
 private fun rights(value:MdmRights){
  fun check(right:MdmRight,label:String,selected:Boolean){
   val box=CheckBox(this).apply{text=label;isChecked=selected}
   checks[right]=box;panel.addView(box)
  }
  check(MdmRight.CONFIG,"Изменять конфигурацию приложения",value.config)
  check(MdmRight.VPN,"Включать и выключать VPN",value.vpn)
  check(MdmRight.TELEMETRY,"Собирать телеметрию",value.telemetry)
  check(MdmRight.GEO,"Передавать данные Wi-Fi и сот, связанные с местоположением",value.geo)
  check(MdmRight.COORDINATES,"Передавать координаты при включении трекинга",value.coordinates)
  panel.addView(ui.text("Доступ к локальной сети через телефон"))
  lan=Spinner(this).apply{
   adapter=ArrayAdapter(this@MdmEnrollActivity,android.R.layout.simple_spinner_dropdown_item,
    listOf("Не разрешать","Подтверждать каждую сессию","Разрешить без подтверждения"))
   setSelection(listOf("deny","confirm","allow").indexOf(value.lanMode))
  }
  panel.addView(lan)
 }
 private fun selectedRights()=MdmRights(
  checks[MdmRight.CONFIG]!!.isChecked,checks[MdmRight.VPN]!!.isChecked,
  checks[MdmRight.TELEMETRY]!!.isChecked,checks[MdmRight.GEO]!!.isChecked,
  checks[MdmRight.COORDINATES]!!.isChecked,listOf("deny","confirm","allow")[lan.selectedItemPosition])
 private fun work(action:()->Unit){
  if(busy)return
  busy=true;status.text="Выполняется…"
  Thread{
   val result=runCatching{action()}
   runOnUiThread{
    busy=false
    if(!isFinishing && !isDestroyed){
     runCatching{render()}
     if(result.isFailure)status.text="Операция не завершена. Проверьте соединение и повторите. Сохранённое состояние: "+if(runCatching{MdmRuntime.controller(this).read().active}.getOrDefault(false))"MDM включено" else "MDM неактивно"
    }
   }
  }.start()
 }
}
