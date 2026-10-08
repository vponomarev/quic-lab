package ru.vpnc.quiclab

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.*

class AppSettingsActivity:Activity(){
 private lateinit var ui:ClientUi
 private lateinit var rtt:PreferenceDraft
 private lateinit var reserve:PreferenceDraft
 private lateinit var budget:PreferenceDraft
 private lateinit var meta:PreferenceDraft
 private lateinit var limit:EditText
 private var initialLimit=""
 private var initialising=true
 private var busy=false
 private val handler=Handler(Looper.getMainLooper())
 override fun onCreate(savedInstanceState:Bundle?){
  super.onCreate(savedInstanceState);ui=ClientUi(this)
  rtt=PreferenceDraft(VpnRttSettings.preferences(this));reserve=PreferenceDraft(VpnReserveSettings.preferences(this));budget=PreferenceDraft(VpnBudgetSettings.preferences(this));meta=PreferenceDraft(VpnProfiles.meta(this))
  // Normalize displayed defaults in the private draft before Spinner callbacks run.
  // Initial selection can be delivered after the first posted initialization task.
  meta.edit().putString("dns_mode",if(meta.getString("dns_mode","tunnel")=="system")"system" else "tunnel").apply()
  for((key,default) in listOf("screen_on" to 1000L,"screen_off" to 0L)) {
   val displayed=VpnRttSettings.values.indexOf(rtt.getLong(key,default)).coerceAtLeast(0)
   rtt.edit().putLong(key,VpnRttSettings.values[displayed]).apply()
  }
  for((key,default) in listOf("wifi_on" to true,"wifi_off" to true,"cell_on" to false,"cell_off" to false,"metered_wifi" to false))
   reserve.edit().putBoolean(key,reserve.getBoolean(key,default)).apply()
  val root=ui.column();val bar=LinearLayout(this);bar.addView(ui.button("Назад"){leave()});bar.addView(ui.text("Общие настройки",20f,true));root.addView(bar)
  val panel=ui.column().apply{setPadding(ui.dp(16),0,ui.dp(16),ui.dp(16))};root.addView(ScrollView(this).apply{addView(panel)},LinearLayout.LayoutParams(-1,0,1f))
  var box=ui.card(panel);box.addView(ui.text("DNS",18f,true));val dns=Spinner(this);dns.adapter=ArrayAdapter(this,android.R.layout.simple_spinner_dropdown_item,arrayOf("Через выбранный VPN-выход","Системный DNS сети"));dns.setSelection(if(meta.getString("dns_mode","tunnel")=="system")1 else 0);box.addView(dns);dns.onItemSelectedListener=object:android.widget.AdapterView.OnItemSelectedListener{override fun onNothingSelected(p:android.widget.AdapterView<*>?){};override fun onItemSelected(p:android.widget.AdapterView<*>?,v:android.view.View?,pos:Int,id:Long){meta.edit().putString("dns_mode",if(pos==1)"system" else "tunnel").apply()}}
  box.addView(ui.text("Системный DNS работает вне туннеля. Изменение DNS требует переподключения VPN."))
  box=ui.card(panel);box.addView(ui.text("Общий лимит LTE",18f,true));box.addView(ui.text("МиБ за период VPN · 0 — без ограничения"));limit=EditText(this).apply{inputType=android.text.InputType.TYPE_CLASS_NUMBER;setText(budget.getLong("cell_mib",0).toString())};box.addView(limit);initialLimit=limit.text.toString();box.addView(ui.text("Общий для всех выходов. Изменение применяется после перезапуска VPN. Это счётчик приложения, а не оператора."))
  box.addView(ui.text("После аварии расход восстанавливается с последней записи. Обычное выключение VPN начинает новый период."))
  box.addView(ui.button("Начать новый LTE-период"){
   if(LabVpnService.active){Toast.makeText(this,"Сначала выключите VPN",Toast.LENGTH_LONG).show()}
   else AlertDialog.Builder(this).setTitle("Сбросить расход LTE?").setMessage("Сохранённый расход будет удалён, включая повреждённую запись. Следующее включение VPN начнёт новый период.").setNegativeButton("Отмена",null).setPositiveButton("Сбросить"){_,_->
    startService(Intent(this,LabVpnService::class.java).setAction("stop"))
   }.show()
  })
  box=ui.card(panel);box.addView(ui.text("Измерения задержки",18f,true))
  for((key,title,default) in listOf(Triple("screen_on","При включённом экране",1000L),Triple("screen_off","При выключенном экране",0L))){box.addView(ui.text(title));val spinner=Spinner(this);spinner.adapter=ArrayAdapter(this,android.R.layout.simple_spinner_dropdown_item,VpnRttSettings.labels);spinner.setSelection(VpnRttSettings.values.indexOf(rtt.getLong(key,default)).coerceAtLeast(0));box.addView(spinner);spinner.onItemSelectedListener=object:android.widget.AdapterView.OnItemSelectedListener{override fun onNothingSelected(p:android.widget.AdapterView<*>?){};override fun onItemSelected(p:android.widget.AdapterView<*>?,v:android.view.View?,pos:Int,id:Long){rtt.edit().putLong(key,VpnRttSettings.values[pos]).apply()}}}
  box.addView(ui.text("После сохранения применяется сразу. Отключение измерений не отключает служебные проверки и смену сети."))
  box=ui.card(panel);box.addView(ui.text("Резервные сети",18f,true))
  for((key,title,default) in listOf(Triple("wifi_on","Проверять резерв Wi-Fi · экран включён",true),Triple("wifi_off","Проверять резерв Wi-Fi · экран выключен",true),Triple("cell_on","Готовить резерв LTE · экран включён",false),Triple("cell_off","Готовить резерв LTE · экран выключен",false),Triple("metered_wifi","Фоновые проверки лимитного Wi-Fi",false))){box.addView(Switch(this).apply{text=title;isChecked=reserve.getBoolean(key,default);setOnCheckedChangeListener{_,checked->reserve.edit().putBoolean(key,checked).apply()}})}
  box.addView(ui.text("Проверки расходуют трафик и батарею. При потере сети VPN может перейти на LTE даже без фонового резерва. При работе через LTE появившийся Wi-Fi проверяется перед возвратом."))
  box=ui.card(panel);box.addView(ui.text("Фоновая работа VPN",18f,true))
  box.addView(ui.text("На Xiaomi/MIUI разрешите автозапуск приложения в системных настройках. Очистка памяти может завершить VPN даже с постоянным уведомлением. При необходимости закрепите приложение в недавних и снимите ограничения батареи. Разрешение автозапуска не включает VPN после перезагрузки: подключение запускаете вы."))
  box.addView(ui.button("Системные настройки приложения"){
   runCatching{startActivity(Intent(android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS,android.net.Uri.parse("package:$packageName")))}
    .onFailure{Toast.makeText(this,"Откройте настройки Android → Приложения → QUIC Lab",Toast.LENGTH_LONG).show()}
  })
  panel.addView(ui.button("Управление устройством (MDM)"){startActivity(Intent(this,MdmEnrollActivity::class.java))})
  panel.addView(ui.button("Диагностика и отправка журналов"){startActivity(Intent(this,DiagnosticsSettingsActivity::class.java))})
  root.addView(ui.button("Сохранить",true){save{}});EditorViewState.assign(root);ui.install(root);listOf(rtt,reserve,budget,meta).forEach{it.acceptInitialState()};initialising=false
 }
 private fun dirty()=!initialising && (listOf(rtt,reserve,budget,meta).any{it.dirty} || limit.text.toString()!=initialLimit)
 private fun leave(){if(busy)return;if(!dirty()){finish();return};AlertDialog.Builder(this).setTitle("Сохранить изменения?").setPositiveButton("Сохранить"){_,_->save{finish()}}.setNegativeButton("Не сохранять"){_,_->finish()}.setNeutralButton("Продолжить редактирование",null).show()}
 private fun error(e:Exception){AlertDialog.Builder(this).setTitle("Не удалось сохранить").setMessage(e.message).setPositiveButton("Понятно",null).show()}
 private fun save(after:()->Unit){if(busy)return;if(!dirty()){Toast.makeText(this,"Нет изменений",Toast.LENGTH_SHORT).show();after();return};try{
  val value=limit.text.toString().toLongOrNull();require(value!=null && value in 0..1048576){"Лимит LTE: целое число от 0 до 1048576 МиБ"};budget.edit().putLong("cell_mib",value).apply()
  val restart=LabVpnService.active && (budget.dirty || meta.dirty)
  fun persist(){synchronized(MdmConfiguration.lock){listOf(rtt,reserve,budget,meta).forEach{it.validateWrite()};check(!restart || !LabVpnService.active){"Сначала остановите VPN"};listOf(rtt,reserve,budget,meta).forEach{it.persist()};initialLimit=limit.text.toString();Toast.makeText(this,"Настройки сохранены",Toast.LENGTH_SHORT).show();if(restart)startForegroundService(Intent(this,LabVpnService::class.java));after()}}
  if(restart){AlertDialog.Builder(this).setTitle("Применить и переподключить?").setMessage("Перезапустятся все VPN-выходы. Текущие соединения прервутся, лимит LTE начнёт отсчёт заново.").setNegativeButton("Отмена",null).setPositiveButton("Применить и переподключить"){_,_->busy=true;requestedOrientation=android.content.pm.ActivityInfo.SCREEN_ORIENTATION_LOCKED;startService(Intent(this,LabVpnService::class.java).setAction("stop"));val deadline=android.os.SystemClock.elapsedRealtime()+10000;handler.post(object:Runnable{override fun run(){if(isDestroyed)return;if(LabVpnService.active && android.os.SystemClock.elapsedRealtime()<deadline){handler.postDelayed(this,100);return};busy=false;requestedOrientation=android.content.pm.ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED;try{persist()}catch(e:Exception){error(e)}}})}.show()}else persist()
 }catch(e:Exception){error(e)}}
 override fun onSaveInstanceState(out:Bundle){
  listOf(rtt,reserve,budget,meta).forEachIndexed{i,d->out.putSerializable("draft_$i",d.snapshot())};super.onSaveInstanceState(out)
 }
 @Suppress("DEPRECATION","UNCHECKED_CAST") override fun onRestoreInstanceState(saved:Bundle){handler.post{
  super.onRestoreInstanceState(saved)
  listOf(rtt,reserve,budget,meta).forEachIndexed{i,d->(saved.getSerializable("draft_$i") as? Map<String, *>)?.let{d.restore(it)}}
 }}
 @Deprecated("Legacy back dispatch") override fun onBackPressed(){leave()}
 override fun onDestroy(){handler.removeCallbacksAndMessages(null);super.onDestroy()}
}
