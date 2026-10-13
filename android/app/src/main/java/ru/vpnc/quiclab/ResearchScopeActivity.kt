package ru.vpnc.quiclab

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.os.Bundle
import android.widget.*

class ResearchScopeActivity:Activity() {
 private lateinit var status:TextView
 private lateinit var directConsent:CheckBox
 private lateinit var chooseApps:Button
 private lateinit var applyScope:Button
 private lateinit var modes:Spinner
 private var packages=emptySet<String>()
 private val values=listOf("vpn_only","vpn_plus_apps","all_apps")
 override fun onCreate(saved:Bundle?) {super.onCreate(saved);render(saved)}
 override fun onResume(){super.onResume();refreshConsent()}
 private fun refreshConsent(){
  if(!::directConsent.isInitialized)return
  val state=MdmStore(this).read()
  directConsent.isChecked=if(state.binding!=null)state.rights.researchDirect else state.standaloneResearchDirect
  val controlled=state.pendingEnrollment || (state.binding!=null && state.active && state.rights.config)
  modes.isEnabled=!controlled;chooseApps.isEnabled=!controlled;applyScope.isEnabled=!controlled
  status.text=if(controlled)"Охватом управляет MDM. Измените режим в веб-редакторе устройства." else if(ResearchSettings.supported(this))"Дополнительных приложений: ${packages.size}" else "Расширенный анализ недоступен для выбранных транспортов. Список сохранён."
 }
 private fun render(saved:Bundle?=null) {
  val ui=ClientUi(this);val root=ui.column();ui.install(root)
  root.addView(ui.button("Назад"){finish()})
  val panel=ui.column();root.addView(ScrollView(this).apply{addView(panel)})
  val card=ui.card(panel);card.addView(ui.text("Охват исследования",22f,true))
  val state=MdmStore(this).read();val scope=state.researchScope
  packages=saved?.getStringArrayList("packages")?.toSet()?:scope.packages
  directConsent=CheckBox(this).apply{
   text="Анализ трафика вне VPN";isChecked=if(state.binding!=null)state.rights.researchDirect else state.standaloneResearchDirect
   setOnClickListener {
    val current=MdmStore(this@ResearchScopeActivity).read()
    val managed=current.binding!=null || current.pendingEnrollment
    if(managed){startActivity(Intent(this@ResearchScopeActivity,MdmEnrollActivity::class.java));isChecked=current.rights.researchDirect}
    else {
     val enabled=isChecked
     Thread{val result=runCatching{MdmStore(this@ResearchScopeActivity).setStandaloneResearchDirect(enabled);ResearchSettings.changed()}
      runOnUiThread{if(!isDestroyed){if(result.isFailure)status.text="Не удалось сохранить согласие" else {if(!enabled)modes.setSelection(0);status.text="Согласие сохранено"}}}
     }.start()
    }
   }
  };card.addView(directConsent)
  card.addView(ui.text("Дополнительное разрешение на отправку имён приложений, адресов, объёмов и открытого DNS прямых соединений. Основное согласие на исследование также должно быть включено. DoH/DoT не расшифровываются."))
  modes=Spinner(this).apply{adapter=ArrayAdapter(this@ResearchScopeActivity,android.R.layout.simple_spinner_dropdown_item,listOf("Только VPN","VPN + выбранные приложения","Все приложения"));setSelection(saved?.getInt("mode")?:values.indexOf(scope.mode))}
  card.addView(modes)
  chooseApps=ui.button("Выбрать приложения для анализа"){
   startActivityForResult(Intent(this,AppSelectionActivity::class.java).putExtra("title","Приложения для анализа").putExtra("explanation","Этот список дополняет VPN-трафик. Выбранные приложения сохраняют свой маршрут.").putStringArrayListExtra("apps",ArrayList(packages)),21)
  };card.addView(chooseApps)
  card.addView(ui.text("Только при включённом VPN, для QUIC/HTTPS. Прямые соединения идут через Wi-Fi/LTE без VPN-сервера. Системный DNS следует общей DNS-политике; его приложение может быть неизвестно. IPv6 в перехваченном трафике блокируется."))
  status=ui.text(if(ResearchSettings.supported(this))"Дополнительных приложений: ${packages.size}" else "Расширенный анализ недоступен для выбранных транспортов. Список сохранён.");card.addView(status)
  applyScope=ui.button("Применить",true){
   val apply={save()}
   if(LabVpnService.active)AlertDialog.Builder(this).setTitle("Применить охват?").setMessage("Для изменения списка перехватываемых приложений VPN кратковременно переподключится.").setNegativeButton("Отмена",null).setPositiveButton("Применить"){_,_->apply()}.show() else apply()
  };card.addView(applyScope)
 }
 private fun save() {
  val scope=ResearchScope(values[modes.selectedItemPosition],packages)
  Thread{
   val result=runCatching{
    val state=MdmStore(this).read()
    check(!(state.active && state.rights.config)){"Охватом управляет MDM"}
    if(scope.mode!="vpn_only") {
     val ids=if(VpnProfiles.multiple(this))VpnProfiles.enabled(this) else setOf(VpnProfiles.current(this).id)
     check(ids.isNotEmpty() && ids.all{VpnProfiles.preferences(this,it).getString("transport","quic") in setOf("quic","https")}){"Расширенный анализ доступен только для QUIC/HTTPS"}
    }
    MdmStore(this).setResearchScope(scope);ResearchSettings.changed()
   }
   runOnUiThread{if(!isDestroyed){status.text=result.exceptionOrNull()?.message?:"Настройки сохранены";if(result.isSuccess && LabVpnService.active)startForegroundService(Intent(this,LabVpnService::class.java).setAction("research-reconfigure"))}}
  }.start()
 }
 override fun onActivityResult(requestCode:Int,resultCode:Int,data:Intent?){super.onActivityResult(requestCode,resultCode,data);if(requestCode==21 && resultCode==RESULT_OK){packages=data?.getStringArrayListExtra("apps")?.toSet()?:packages;status.text="Дополнительных приложений: ${packages.size}"}}
 override fun onSaveInstanceState(out:Bundle){out.putStringArrayList("packages",ArrayList(packages));out.putInt("mode",modes.selectedItemPosition);super.onSaveInstanceState(out)}
}
