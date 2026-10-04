package ru.vpnc.quiclab

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.net.VpnService
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.widget.*

/** Stable navigation shell. Viewing a connection never selects or starts it. */
class VpnActivity:Activity() {
 private lateinit var ui:ClientUi
 private lateinit var content:LinearLayout
 private lateinit var scroll:ScrollView
 private lateinit var heading:TextView
 private val handler=Handler(Looper.getMainLooper())
 private var page="VPN"
 private val positions=mutableMapOf<String,Int>()
 private val nav=linkedMapOf<String,Button>()
 private var status:TextView?=null
 private var control:Button?=null
 private var budget:TextView?=null
 private var exits:LinearLayout?=null
 private var logs:TextView?=null
 private var radios:RadioMonitor?=null
 private var wifiRadio="Wi-Fi: сведения недоступны"
 private var cellRadio="Сота: сведения недоступны"
 private var radioSummary:TextView?=null
 private var metricRun:String?=null
 private val labelsCache=mutableMapOf<String,Pair<Long,Map<String,String>>>()
 private val metricViews=linkedMapOf<String,TextView>()
 private val ticker=object:Runnable{override fun run(){refreshStatus();handler.postDelayed(this,1000)}}
 override fun onCreate(savedInstanceState:Bundle?){
  super.onCreate(savedInstanceState);Diagnostics.init(applicationContext);ui=ClientUi(this)
  page=savedInstanceState?.getString("page") ?: "VPN"
  for(name in listOf("VPN","Подключения","Диагностика","Настройки")) positions[name]=savedInstanceState?.getInt("scroll_$name") ?: 0
  val root=ui.column();heading=ui.text(page,25f,true).apply{setPadding(ui.dp(20),ui.dp(14),ui.dp(20),ui.dp(12))};root.addView(heading)
  content=ui.column().apply{setPadding(ui.dp(16),0,ui.dp(16),ui.dp(20))};scroll=ScrollView(this).apply{isFillViewport=true;addView(content)};root.addView(scroll,LinearLayout.LayoutParams(-1,0,1f))
  val bottom=LinearLayout(this).apply{setPadding(ui.dp(6),ui.dp(6),ui.dp(6),ui.dp(6))}
  for(name in listOf("VPN","Подключения","Диагностика","Настройки")){val b=ui.button(name){showPage(name)}.apply{textSize=10f;setSingleLine();setPadding(ui.dp(2),ui.dp(8),ui.dp(2),ui.dp(8))};nav[name]=b;bottom.addView(b,LinearLayout.LayoutParams(0,ui.dp(54),1f))}
  root.addView(bottom);ui.install(root);showPage(page,false)
 }
 private fun action(block:()->Unit){try{block()}catch(e:Exception){AlertDialog.Builder(this).setTitle("Не удалось выполнить действие").setMessage(e.message ?: "Повторите попытку").setPositiveButton("Понятно",null).show()}}
 private fun showPage(next:String,remember:Boolean=true){
  if(remember)positions[page]=scroll.scrollY
  page=next;heading.text=page;content.removeAllViews();status=null;control=null;budget=null;exits=null;logs=null;radioSummary=null;metricViews.clear()
  nav.forEach{(name,b)->b.setTextColor(if(name==page)ui.accent else ui.muted);b.alpha=if(name==page)1f else .7f}
  action{when(page){"VPN"->home();"Подключения"->connections();"Диагностика"->diagnostics();else->settings()}}
  scroll.post{scroll.scrollTo(0,positions[page] ?: 0)};refreshStatus()
 }
 private fun home(){
  val card=ui.card(content);status=ui.text("",23f,true);card.addView(status)
  val selected=VpnProfiles.current(this)
  card.addView(ui.text(if(VpnProfiles.multiple(this))"Одновременные подключения" else selected.name,17f,true))
  card.addView(ui.text(if(VpnProfiles.multiple(this))"Выбрано выходов: ${VpnProfiles.enabled(this).size}" else VpnProfiles.preferences(this).getString("transport","quic").orEmpty().let{if(it=="awg")"AmneziaWG" else it.uppercase()}))
  control=ui.button("Подключить",true){action{if(LabVpnService.active)startService(Intent(this,LabVpnService::class.java).setAction("stop")) else requestStart()}};ui.add(card,control!!)
  ui.add(card,ui.button("Выбрать подключение"){showPage("Подключения")})
  budget=ui.text("");content.addView(budget)
  content.addView(ui.text("Подключения и трафик",16f,true))
  radioSummary=ui.text("",12f).apply{setTextColor(ui.ink);setPadding(0,ui.dp(4),0,ui.dp(12))};content.addView(radioSummary)
  exits=ui.column();content.addView(exits)
 }
 private fun requestStart(){
  if(VpnProfiles.multiple(this))MultipleVpnPlan.load(this) else {VpnIdentity.load(this);VpnConfiguration.load(this)}
  val request=VpnService.prepare(this);if(request!=null)startActivityForResult(request,12) else startForegroundService(Intent(this,LabVpnService::class.java))
 }
 private fun connections(){
  ui.add(content,ui.button("Добавить подключение",true){addConnection()})
  ui.add(content,ui.button("Одновременные подключения"){startActivity(Intent(this,MultipleVpnActivity::class.java))})
  content.addView(ui.text("Откройте профиль для просмотра. Для запуска выберите его отдельной кнопкой."))
  val selected=VpnProfiles.current(this).id
  for(p in VpnProfiles.list(this)){
   val card=ui.card(content);card.addView(ui.text(p.name,17f,true));val prefs=VpnProfiles.preferences(this,p.id)
   card.addView(ui.text(listOf(prefs.getString("transport","quic").orEmpty().uppercase(),prefs.getString("endpoint","").orEmpty()).joinToString(" · ")))
   card.addView(ui.text(if(p.id==selected)"Выбрано для запуска" else "Сохранённое подключение"))
   val row=LinearLayout(this);card.addView(row)
   row.addView(ui.button("Настроить"){openEditor(p.id)},LinearLayout.LayoutParams(0,-2,1f))
   row.addView(ui.button(if(selected==p.id)"Выбрано" else "Выбрать"){action{check(!LabVpnService.active){"Для смены подключения сначала отключите VPN"};VpnProfiles.select(this,p.id);VpnProfiles.setMultiple(this,false);showPage(page,false)}},LinearLayout.LayoutParams(0,-2,1f))
   ui.add(card,ui.button("Действия с подключением"){AlertDialog.Builder(this).setTitle(p.name).setItems(arrayOf("Переименовать","Удалить")){_,i->if(i==0)rename(p) else delete(p)}.show()})
  }
 }
 private fun openEditor(id:String){startActivity(Intent(this,ProfileEditorActivity::class.java).putExtra("profile_id",id))}
 private fun rename(p:VpnProfiles.Profile){val input=EditText(this).apply{setText(p.name);setSingleLine()};AlertDialog.Builder(this).setTitle("Название подключения").setView(input).setNegativeButton("Отмена",null).setPositiveButton("Сохранить"){_,_->action{VpnProfiles.rename(this,input.text.toString(),p.id);showPage(page,false)}}.show()}
 private fun delete(p:VpnProfiles.Profile){AlertDialog.Builder(this).setTitle("Удалить ${p.name}?").setMessage("Профиль и его данные доступа будут удалены с телефона.").setNegativeButton("Отмена",null).setPositiveButton("Удалить"){_,_->action{VpnProfiles.delete(this,p.id);showPage(page,false)}}.show()}
 private fun addConnection(){
  AlertDialog.Builder(this).setTitle("Добавить подключение").setItems(arrayOf("Сканировать QR","Вставить ссылку / конфиг","Выбрать файл JSON / CONF","Настроить вручную")){_,which->action{
   check(!LabVpnService.active){"Для импорта сначала отключите VPN"}
   when(which){0->startActivityForResult(Intent(this,ProfileScanActivity::class.java),ProfileImport.REQUEST);1->pasteConnection();2->startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT).setType("*/*").addCategory(Intent.CATEGORY_OPENABLE),14);else->{val input=EditText(this);AlertDialog.Builder(this).setTitle("Название подключения").setView(input).setNegativeButton("Отмена",null).setPositiveButton("Добавить"){_,_->action{openEditor(VpnProfiles.create(this,input.text.toString()).id)}}.show()}}
  }}.show()
 }
 private fun pasteConnection(){
  val input=EditText(this).apply{hint="https://…/enroll#… или vless://… / JSON";minLines=3;maxLines=8;isSaveEnabled=false;importantForAutofill=android.view.View.IMPORTANT_FOR_AUTOFILL_NO;inputType=android.text.InputType.TYPE_CLASS_TEXT or android.text.InputType.TYPE_TEXT_FLAG_MULTI_LINE or android.text.InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS}
  AlertDialog.Builder(this).setTitle("Вставить ссылку / конфиг").setView(input).setNegativeButton("Отмена"){_,_->input.text.clear()}.setPositiveButton("Проверить"){_,_->val raw=input.text.toString().trim();input.text.clear();startActivityForResult(Intent(this,ProfileScanActivity::class.java).putExtra("import_text",raw),ProfileImport.REQUEST)}.show()
 }
 private fun diagnostics(){
  val card=ui.card(content);card.addView(ui.text("Проверка связи",18f,true));card.addView(ui.text("Echo проверяет передачу данных через выход. Самостоятельное сравнение транспортов доступно внутри диагностики."))
  ui.add(card,ui.button("Echo · диагностика сетей"){startActivity(Intent(this,EchoActivity::class.java))})
  ui.add(card,ui.button("Отчёт для диагностики"){Diagnostics.preview(this,"VPN: ${LabVpnService.status}")})
  content.addView(ui.text("Последние события",16f,true));logs=ui.text("");content.addView(logs)
 }
 private fun settings(){
  val card=ui.card(content);card.addView(ui.text("Общие настройки",18f,true));card.addView(ui.text("Сети, лимит LTE, DNS и частота измерений действуют для всех подключений."))
  ui.add(card,ui.button("Сети, трафик и измерения"){startActivity(Intent(this,AppSettingsActivity::class.java))})
  ui.add(card,ui.button("Разрешить сведения о сети"){requestPermissions(arrayOf(android.Manifest.permission.ACCESS_COARSE_LOCATION,android.Manifest.permission.ACCESS_FINE_LOCATION),21)})
  ui.add(card,ui.button("Разрешения приложения"){startActivity(Intent(android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS,android.net.Uri.parse("package:$packageName")))})
  val about=ui.card(content);about.addView(ui.text("О приложении",18f,true));about.addView(ui.text("Версия ${BuildConfig.VERSION_NAME}\nIPv4 · IPv6 для трафика VPN блокируется\nЗакрытие экрана не отключает работающий VPN."))
 }
 private fun setText(view:TextView?,value:String){if(view?.text?.toString()!=value)view?.text=value}
 private fun refreshStatus(){
  setText(status,if(!LabVpnService.active)"Отключено" else LabVpnService.status)
  setText(control,if(LabVpnService.active)"Отключить" else "Подключить")
  setText(budget,VpnDashboardText.budget(VpnDashboardEvents.budget()))
  setText(logs,LabVpnService.log())
  val summary=RadioSummary.text(wifiRadio,cellRadio);if(radioSummary?.text?.toString()!=summary.toString())radioSummary?.text=summary
  val parent=exits ?: return
  if(metricRun!=VpnDashboardEvents.runId){metricRun=VpnDashboardEvents.runId;labelsCache.clear()}
  val cards=VpnDashboardEvents.dashboards();val names=VpnDashboardEvents.names();val ids=cards.map{it.exitId}.toSet()
  metricViews.keys.filter{it !in ids && !(it.isEmpty() && cards.isEmpty())}.toList().forEach{parent.removeView(metricViews.remove(it))}
  if(cards.isEmpty()){if(!metricViews.containsKey("")){val v=ui.text("Выберите подключение и нажмите «Подключить».");metricViews[""]=v;parent.addView(v)};return}
  for(card in cards){val v=metricViews.getOrPut(card.exitId){ui.text("",15f).apply{setPadding(ui.dp(16),ui.dp(12),ui.dp(16),ui.dp(12));background=ui.background(android.graphics.Color.WHITE);parent.addView(this,LinearLayout.LayoutParams(-1,-2).apply{bottomMargin=ui.dp(12)})}}
   if(labelsCache[card.exitId]?.first!=card.generation){
    val kind=VpnProfiles.preferences(this,card.exitId).getString("transport","quic").orEmpty()
    val selected=if(kind=="awg")"AmneziaWG" else kind.uppercase()
    labelsCache[card.exitId]=card.generation to mapOf("" to selected,card.exitId to selected,"${card.exitId}.quic" to "QUIC","${card.exitId}.https" to "HTTPS","${card.exitId}.vless" to "VLESS","${card.exitId}.awg" to "AmneziaWG")
   }
   val labels=labelsCache.getValue(card.exitId).second
   setText(v,VpnDashboardText.card(card,names[card.exitId] ?: "Подключение",SystemClock.elapsedRealtime(),VpnRttSettings.interval(this),"",labels))
  }
 }
 override fun onResume(){super.onResume();showPage(page,false);handler.removeCallbacks(ticker);handler.post(ticker);radios=RadioMonitor(this,{wifi,cell->wifiRadio=wifi;cellRadio=cell;refreshStatus()},{_,_->}).also{it.start()}}
 override fun onPause(){positions[page]=scroll.scrollY;handler.removeCallbacks(ticker);radios?.close();radios=null;super.onPause()}
 override fun onSaveInstanceState(out:Bundle){out.putString("page",page);positions[page]=scroll.scrollY;positions.forEach{(name,pos)->out.putInt("scroll_$name",pos)};super.onSaveInstanceState(out)}
 @Deprecated("Legacy back dispatch") override fun onBackPressed(){if(page!="VPN")showPage("VPN") else super.onBackPressed()}
 override fun onActivityResult(requestCode:Int,resultCode:Int,data:Intent?){super.onActivityResult(requestCode,resultCode,data);if(resultCode!=RESULT_OK)return
  action{when(requestCode){12->startForegroundService(Intent(this,LabVpnService::class.java));ProfileImport.REQUEST->showPage("Подключения");14->{val uri=data?.data ?: return@action;val raw=contentResolver.openInputStream(uri)!!.use{val bytes=ByteArray(32769);var count=0;while(count<bytes.size){val n=it.read(bytes,count,bytes.size-count);if(n<0)break;count+=n};require(count<=32768){"Конфигурация слишком большая"};String(bytes,0,count,Charsets.UTF_8)}
   when{raw.trimStart().startsWith("vless://")->VlessImport.review(this,raw,{showPage("Подключения")});raw.trimStart().startsWith("{")->{val p=org.json.JSONObject(raw);if(p.optString("protocol")=="vless")VlessImport.review(this,raw,{showPage("Подключения")}) else {require(ProfileImport.validate(p)=="vpn"){"Нужен профиль VPN"};AlertDialog.Builder(this).setTitle("Добавить подключение?").setMessage(ProfileImport.reviewAddress(p)).setNegativeButton("Отмена",null).setPositiveButton("Добавить"){_,_->action{ProfileImport.save(this,p);showPage("Подключения")}}.show()}};else->AwgImport.review(this,raw,{showPage("Подключения")})}
  }}}
 }
}
