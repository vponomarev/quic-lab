package ru.vpnc.quiclab
import android.app.Activity
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.*
import org.json.JSONObject
internal fun probeLabel(s:String)=mapOf("success" to "Работает","relay_failed" to "HTTPS работает","relay_auth" to "Общий выход недоступен","not_applicable" to "—","queued" to "Ожидание","running" to "Проверяется","finished" to "Завершено","timeout" to "Тайм-аут","dns" to "Ошибка DNS","transport" to "Ошибка обмена","target_tls" to "Ошибка TLS цели","unexpected_response" to "Неожиданный ответ","budget" to "Лимит LTE","canceled" to "Отменено","interrupted" to "Прервано","network_changed" to "Сеть сменилась","configuration_changed" to "Настройки изменились","busy" to "Проверка ещё выполняется","needs_foreground" to "Откройте приложение","no_network" to "Нет сети")[s]?:s
class VlessProbeActivity:Activity(){
 private lateinit var body:LinearLayout;private lateinit var table:LinearLayout;private lateinit var status:TextView;private lateinit var choose:Spinner;private lateinit var filter:Spinner;private lateinit var sort:Spinner
 private var sources=emptyList<ProbeSource>();private var message="";private val handler=Handler(Looper.getMainLooper());private val worker=java.util.concurrent.Executors.newSingleThreadExecutor()
 private val refresh=object:Runnable{override fun run(){render();handler.postDelayed(this,1000)}}
 override fun onCreate(b:Bundle?){super.onCreate(b);body=LinearLayout(this).apply{orientation=LinearLayout.VERTICAL;setPadding(20,16,20,16)};setContentView(ScrollView(this).apply{addView(body)})
  text("Проверка серверов VLESS",22f);text("Проверяет телефон. Только импортированные узлы; рабочий VPN не переключается.")
  sources=VlessProbeCoordinator.sources(this);choose=Spinner(this);choose.adapter=ArrayAdapter(this,android.R.layout.simple_spinner_dropdown_item,sources.map{it.name});body.addView(choose)
  val initial=intent.getStringExtra("source");sources.indexOfFirst{it.id==initial}.takeIf{it>=0}?.let{choose.setSelection(it)}
  val controls=LinearLayout(this);body.addView(controls)
  for((title,action) in listOf("Все" to "start","Неудачные" to "retry","Стоп" to "cancel")){controls.addView(Button(this).apply{text=title;setOnClickListener{val source=sources.getOrNull(choose.selectedItemPosition)?:return@setOnClickListener;val previous=VlessProbeCoordinator.results(this@VlessProbeActivity).optJSONObject(source.id)?.optString("runId").orEmpty();isEnabled=false;worker.execute{val result=runCatching{if(action=="cancel"){if(VlessProbeCoordinator.cancel(previous))"canceled" else "not_running"}else VlessProbeCoordinator.start(this@VlessProbeActivity,source.id,action,previous)}.getOrDefault("probe_failed");runOnUiThread{isEnabled=true;message=probeLabel(result);render()}}}},LinearLayout.LayoutParams(0,-2,1f))}
  filter=Spinner(this);filter.adapter=ArrayAdapter(this,android.R.layout.simple_spinner_dropdown_item,listOf("Все","Работают","Ошибки","Не проверены"));body.addView(filter)
  sort=Spinner(this);sort.adapter=ArrayAdapter(this,android.R.layout.simple_spinner_dropdown_item,listOf("Порядок подписки","По RTT"));body.addView(sort)
  status=text("");table=LinearLayout(this).apply{orientation=LinearLayout.VERTICAL};body.addView(table)
  text("Время проверки включает установление соединения. RTT — обмен по готовому каналу. Успех не гарантирует скорость или UDP.")
 }
 private fun text(value:String,size:Float=14f):TextView=TextView(this).apply{text=value;textSize=size;setPadding(4,6,4,6);body.addView(this)}
 private fun date(v:String)=runCatching{java.text.DateFormat.getDateTimeInstance().format(java.util.Date.from(java.time.Instant.parse(v)))}.getOrDefault("—")
 private fun render(){if(isFinishing||isDestroyed)return;val s=sources.getOrNull(choose.selectedItemPosition);val r=s?.let{VlessProbeCoordinator.results(this).optJSONObject(it.id)};status.text=message+"\n"+(r?.let{probeLabel(it.optString("state"))+" · "+it.optString("network")+" · "+date(it.optString("startedAt"))}?:"Проверок пока нет")
  table.removeAllViews();val rows=r?.optJSONArray("rows")?:return
  var items=(0 until rows.length()).map{rows.getJSONObject(it)}.filter{row->val outcome=row.optString("outcome");val pending=outcome in listOf("queued","running","canceled","interrupted","network_changed","configuration_changed");when(filter.selectedItemPosition){1->outcome=="success";2->outcome!="success"&&!pending;3->pending;else->true}}
  if(sort.selectedItemPosition==1)items=items.sortedBy{it.optLong("echoRttMs",Long.MAX_VALUE)}
  for(row in items){val line=TextView(this);line.setPadding(10,10,10,10);line.setBackgroundColor(if(row.optString("outcome")=="success")0xffe7f5ee.toInt()else 0xfff0f3f5.toInt());val role=when(row.optString("role")){"primary"->" · основной";"reserve"->" · резерв";else->""};line.text=row.optString("name")+role+"\n"+row.optString("endpoint")+"\nHTTPS: "+probeLabel(row.optString("outcome"))+" · Выход: "+probeLabel(row.optString("relayOutcome","not_applicable"))+"\nПроверка: "+row.optLong("totalMs")+" мс · RTT: "+(if(row.has("echoRttMs"))row.optLong("echoRttMs").toString()+" мс · "+row.optInt("echoSucceeded")+"/3"else "—")+"\n"+date(row.optString("measuredAt"));table.addView(line,LinearLayout.LayoutParams(-1,-2).apply{bottomMargin=5})}
 }
 override fun onResume(){super.onResume();VlessProbeCoordinator.foreground=true;handler.post(refresh)}
 override fun onPause(){handler.removeCallbacks(refresh);if(!isChangingConfigurations)VlessProbeCoordinator.foreground=false;super.onPause()}
 override fun onDestroy(){worker.shutdown();super.onDestroy()}
}
