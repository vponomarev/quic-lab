package ru.vpnc.quiclab
import android.app.Activity
import android.app.AlertDialog
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.*

class DiagnosticsSettingsActivity:Activity(){
 private val handler=Handler(Looper.getMainLooper())
 private lateinit var status:TextView
 private val refresh=object:Runnable{override fun run(){render();handler.postDelayed(this,2000)}}
 override fun onCreate(state:Bundle?){
  super.onCreate(state);Diagnostics.init(this)
  val ui=ClientUi(this);val root=ui.column()
  root.addView(ui.button("Назад"){finish()})
  val panel=ui.column();root.addView(ScrollView(this).apply{addView(panel)},LinearLayout.LayoutParams(-1,0,1f))
  panel.addView(ui.text("Диагностика и журналы",22f,true))
  panel.addView(Switch(this).apply{
   text="Диагностика и отправка журналов";isChecked=DiagnosticsPolicy.enabled(this@DiagnosticsSettingsActivity)
   setOnCheckedChangeListener{_,value->DiagnosticsPolicy.preferences(this@DiagnosticsSettingsActivity).edit().putBoolean("enabled",value).commit();DiagnosticsDelivery.schedule(this@DiagnosticsSettingsActivity);render()}
  })
  panel.addView(ui.text("В этой версии включена по умолчанию. Сбор событий и сводок; автоматическая отправка только через безлимитный Wi-Fi. Можно выключить в любой момент. Настройки применяются сразу."))
  panel.addView(Switch(this).apply{
   text="Подробная диагностика потоков";isChecked=DiagnosticsPolicy.detailed(this@DiagnosticsSettingsActivity)
   setOnCheckedChangeListener{_,value->DiagnosticsPolicy.preferences(this@DiagnosticsSettingsActivity).edit().putBoolean("detailed",value).commit();render()}
  })
  panel.addView(ui.text("Подробные журналы содержат адреса назначения. Ключи, конфиги, содержимое пакетов и геоданные не отправляются. События: 7 дней / 5 МиБ; сводки: 3 дня / 10 МиБ; подробные: 24 часа / 20 МиБ. На сервере по умолчанию 14 дней."))
  status=ui.text("");panel.addView(status)
  panel.addView(ui.button("Отправить сейчас",true){
   status.text="Отправка через доступную сеть, включая LTE…"
   DiagnosticsDelivery.manual(this){handler.post{render()}}
  })
  panel.addView(ui.text("Кнопка отправляет все ещё не доставленные журналы, включая ранее собранные подробные. Работает и при выключенном сборе. Может использовать LTE и расходовать трафик вне лимита VPN."))
  panel.addView(ui.button("Удалить локальные журналы"){
   AlertDialog.Builder(this).setTitle("Удалить накопленные журналы?").setMessage("Очередь и локальная история будут удалены. Уже принятые сервером записи останутся там до окончания срока хранения.")
    .setNegativeButton("Отмена",null).setPositiveButton("Удалить"){_,_->Diagnostics.clear();render()}.show()
  })
  ui.install(root)
 }
 private fun render(){
  if(!::status.isInitialized)return
  val c=this
  val destinations=VpnProfiles.list(c).map{p->p.name+": "+runCatching{DiagnosticsDelivery.destination(c,p.id).first}.getOrDefault("нет регистрации для отправки")}
  val prefs=DiagnosticsPolicy.preferences(c);val sent=prefs.getLong("last_sent",0)
  status.text="Получатели:\n"+destinations.joinToString("\n")+"\nОчередь: "+Diagnostics.store().bytes(true)/1024+" КиБ"+
   "\nЛокальная история: "+Diagnostics.store().bytes()/1024+" КиБ"+
   "\nПоследняя доставка: "+if(sent==0L)"ещё не было\n"+prefs.getString("status","Ожидание безлимитного Wi-Fi") else java.text.DateFormat.getDateTimeInstance().format(java.util.Date(sent))+"\n"+prefs.getString("status","")
 }
 override fun onResume(){super.onResume();handler.post(refresh)}
 override fun onPause(){handler.removeCallbacks(refresh);super.onPause()}
}
