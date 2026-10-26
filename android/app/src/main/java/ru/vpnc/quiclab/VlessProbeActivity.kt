package ru.vpnc.quiclab
import android.app.Activity
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.*
import org.json.JSONObject
internal fun probeLabel(s:String)=mapOf("skipped" to "Пропущено","permission_denied" to "Проверка запрещена настройками","not_running" to "Проверка не запущена","probe_failed" to "Не удалось запустить проверку","not_attempted" to "Не проверен","success" to "Работает","relay_failed" to "HTTPS работает","relay_auth" to "Общий выход недоступен","not_applicable" to "—","queued" to "Ожидание","running" to "Проверяется","finished" to "Завершено","timeout" to "Тайм-аут","dns" to "Ошибка DNS","transport" to "Ошибка обмена","target_tls" to "Ошибка TLS цели","unexpected_response" to "Неожиданный ответ","budget" to "Лимит LTE","canceled" to "Отменено","interrupted" to "Прервано","network_changed" to "Сеть сменилась","configuration_changed" to "Настройки изменились","busy" to "Проверка ещё выполняется","needs_foreground" to "Откройте приложение","no_network" to "Нет сети","too_many_nodes" to "Больше 256 узлов — выберите отдельную подписку")[s]?:s
class VlessProbeActivity:Activity(){
 private lateinit var body:LinearLayout
 private lateinit var table:GridLayout
 private lateinit var status:TextView
 private lateinit var choose:Spinner
 private lateinit var networks:Spinner
 private lateinit var filter:Spinner
 private lateinit var sort:Spinner
 private lateinit var scroll:ScrollView
 private var sources=emptyList<ProbeSource>()
 private var preview:JSONObject?=null
 private var message=""
 private var selectedNetwork=""
 private var shownNetworks=emptyList<String>()
 private var signature=""
 private var rendering=false
 private val handler=Handler(Looper.getMainLooper())
 private val worker=java.util.concurrent.Executors.newSingleThreadExecutor()
 private val refresh=object:Runnable{override fun run(){render();handler.postDelayed(this,1000)}}
 override fun onCreate(b:Bundle?){
  super.onCreate(b)
  preview=if(BuildConfig.DEBUG)intent.getStringExtra("probe_preview")?.let{runCatching{JSONObject(it)}.getOrNull()}else null
  selectedNetwork=b?.getString("network").orEmpty()
  body=LinearLayout(this).apply{orientation=LinearLayout.VERTICAL;setPadding(dp(12),dp(10),dp(12),dp(16))}
  scroll=ScrollView(this).apply{addView(body)};setContentView(scroll)
  text("Карта доступности VLESS",22f)
  text(if(preview!=null)"Демонстрационные данные · сеть не используется" else "Wi-Fi → разрешённый LTE. При работе на LTE проверяется только LTE. Рабочий VPN не переключается.")
  if(preview==null){val configured=VlessProbeCoordinator.sources(this);sources=listOfNotNull(allProbeSource(configured))+configured}
  choose=spinner(if(preview!=null)listOf("Пример подписки")else sources.map{it.name})
  val initial=b?.getString("source")?:intent.getStringExtra("source")
  sources.indexOfFirst{it.id==initial}.takeIf{it>=0}?.let{choose.setSelection(it)}
  val controls=LinearLayout(this);body.addView(controls)
  for((title,action) in listOf("Проверить все" to "start","Неудачные" to "retry","Стоп" to "cancel")){
   controls.addView(Button(this).apply{
    text=title;isEnabled=preview==null&&sources.isNotEmpty()
    setOnClickListener{
     val source=sources.getOrNull(choose.selectedItemPosition)?:return@setOnClickListener
     val previous=VlessProbeCoordinator.results(this@VlessProbeActivity).optJSONObject(source.id)?.optString("runId").orEmpty()
     isEnabled=false
     worker.execute{
      val result=runCatching{if(action=="cancel"){if(VlessProbeCoordinator.cancel(previous))"canceled" else "not_running"}else VlessProbeCoordinator.start(this@VlessProbeActivity,source.id,action,previous)}.getOrDefault("probe_failed")
      runOnUiThread{if(!isDestroyed){isEnabled=true;message=probeLabel(result);signature="";render()}}
     }
    }
   },LinearLayout.LayoutParams(0,-2,1f))
  }
  networks=spinner(emptyList()).apply{tag="probe_network";contentDescription="Сеть результатов"}
  filter=spinner(listOf("Все пути","VLESS работает","Ошибки VLESS","Не проверены")).apply{setSelection(b?.getInt("filter")?:0)}
  sort=spinner(listOf("Порядок подписки","По RTT")).apply{setSelection(b?.getInt("sort")?:0)}
  status=text("")
  table=GridLayout(this).apply{columnCount=((resources.displayMetrics.widthPixels/resources.displayMetrics.density-24)/156).toInt().coerceIn(1,4)}
  body.addView(table)
  text("Нажмите путь для подробностей. RTT измеряется по готовому каналу; успешная проверка не гарантирует скорость или UDP.")
  val listener=object:android.widget.AdapterView.OnItemSelectedListener{
   override fun onNothingSelected(parent:android.widget.AdapterView<*>?){}
   override fun onItemSelected(parent:android.widget.AdapterView<*>?,view:android.view.View?,position:Int,id:Long){
    if(parent===networks)selectedNetwork=shownNetworks.getOrNull(position).orEmpty()
    if(!rendering)render()
   }
  }
  listOf(choose,networks,filter,sort).forEach{it.onItemSelectedListener=listener}
  render()
  scroll.post{scroll.scrollTo(0,b?.getInt("scroll")?:0)}
 }
 private fun dp(v:Int)=(v*resources.displayMetrics.density).toInt()
 private fun spinner(values:List<String>)=Spinner(this).apply{
  adapter=ArrayAdapter(this@VlessProbeActivity,android.R.layout.simple_spinner_dropdown_item,values);body.addView(this)
 }
 private fun text(value:String,size:Float=14f)=TextView(this).apply{text=value;textSize=size;setPadding(dp(2),dp(4),dp(2),dp(4));body.addView(this)}
 private fun date(v:String)=runCatching{java.text.DateFormat.getDateTimeInstance().format(java.util.Date.from(java.time.Instant.parse(v)))}.getOrDefault("—")
 private fun networkName(n:String)=when(n){"wifi"->"Wi-Fi";"cell"->"LTE";else->n}
 private fun health(v:ProbeHealth)=when(v){ProbeHealth.LIVE->"работает";ProbeHealth.FAILED->"ошибка";ProbeHealth.UNKNOWN->"не проверен";ProbeHealth.NOT_APPLICABLE->"не настроен"}
 private fun render(){
  if(isFinishing||isDestroyed||rendering)return
  rendering=true
  try{
   val source=sources.getOrNull(choose.selectedItemPosition)
   val run=preview?:source?.let{VlessProbeCoordinator.results(this).optJSONObject(it.id)}
   val available=run?.let{VlessProbePresentation.networks(it)}.orEmpty()
   if(available!=shownNetworks){
    shownNetworks=available
    if(selectedNetwork !in available)selectedNetwork=available.firstOrNull().orEmpty()
    networks.adapter=ArrayAdapter(this,android.R.layout.simple_spinner_dropdown_item,available.map{networkName(it)})
    networks.setSelection(available.indexOf(selectedNetwork).coerceAtLeast(0))
   }
   networks.visibility=if(available.isEmpty())android.view.View.GONE else android.view.View.VISIBLE
   val key=listOf(run?.toString(),source?.id,selectedNetwork,filter.selectedItemPosition,sort.selectedItemPosition,message).joinToString("|")
   if(key==signature)return
   signature=key
   val phase=run?.let{VlessProbePresentation.phase(it,selectedNetwork)}
   val rows=if(run!=null)VlessProbePresentation.rows(run,selectedNetwork)else source?.nodes?.map{it.publicRow()}.orEmpty()
   val counts=VlessProbePresentation.counts(rows)
   val completed=rows.count{it.optString("outcome") !in listOf("queued","running","")}
   val phases=available.joinToString(" · "){n->networkName(n)+": "+probeLabel(VlessProbePresentation.phase(run!!,n)?.optString("state").orEmpty())}
   status.text=listOf(message,if(run==null)"Проверок пока нет" else phases,
    phase?.optString("reason")?.takeIf{it.isNotBlank()}?.let{probeLabel(it)}.orEmpty(),
    "VLESS: работают ${counts[0]} · ошибки ${counts[1]} · не проверены ${counts[2]}",
    if(run==null)"" else "${networkName(selectedNetwork)} · завершено $completed/${rows.size} · ${date(run.optString("startedAt"))}"
   ).filter{it.isNotBlank()}.joinToString("\n")
   table.removeAllViews()
   var items=VlessProbePresentation.filter(rows,filter.selectedItemPosition)
   if(sort.selectedItemPosition==1)items=items.sortedBy{if(it.has("echoRttMs")&&!it.isNull("echoRttMs"))it.optLong("echoRttMs")else Long.MAX_VALUE}
   for(row in items)addTile(row)
   if(items.isEmpty())table.addView(TextView(this).apply{text=if(rows.isEmpty())"Нет измерений для этой сети"else"Нет путей по выбранному фильтру";setPadding(dp(8),dp(12),dp(8),dp(12))})
  }finally{rendering=false}
 }
 private fun addTile(row:JSONObject){
  val availability=VlessProbePresentation.classify(row)
  val color=when{
   availability.vless==ProbeHealth.FAILED->0xffffe8e5.toInt()
   availability.vless==ProbeHealth.LIVE&&availability.ha==ProbeHealth.FAILED->0xfffff0cc.toInt()
   availability.vless==ProbeHealth.LIVE->0xffe3f4e8.toInt()
   else->0xffedf0f4.toInt()
  }
  val name=row.optString("name").ifBlank{row.optString("endpoint")}
  val state=when(row.optString("outcome")){"running"->"проверяется";"queued"->"ожидание";else->health(availability.vless)}
  val rtt=if(row.has("echoRttMs")&&!row.isNull("echoRttMs"))"RTT ${row.optLong("echoRttMs")} мс"else"RTT —"
  val owner=row.optString("sourceName").takeIf{it.isNotBlank()}?.let{"$it\n"}.orEmpty()
  val label="$owner$name\nVLESS: $state\nHA: ${health(availability.ha)}\n$rtt"
  val tile=TextView(this).apply{
   text=label;textSize=13f;setTextColor(0xff172a36.toInt());setPadding(dp(10),dp(10),dp(10),dp(10));minHeight=dp(112)
   background=android.graphics.drawable.GradientDrawable().apply{setColor(color);cornerRadius=dp(10).toFloat()}
   contentDescription=label.replace("\n",", ");isFocusable=true;isClickable=true;setOnClickListener{details(row)}
  }
  table.addView(tile,GridLayout.LayoutParams().apply{width=0;height=-2;columnSpec=GridLayout.spec(GridLayout.UNDEFINED,1f);setMargins(dp(3),dp(3),dp(3),dp(3))})
 }
 private fun details(row:JSONObject){
  val a=VlessProbePresentation.classify(row)
  val detail=TextView(this).apply{
   setPadding(dp(18),dp(10),dp(18),dp(16));setTextIsSelectable(true)
   text=listOf(
    row.optString("sourceName"),
    "Сеть: ${networkName(selectedNetwork)}",
    "Адрес: ${row.optString("endpoint")}",
    "SNI: ${row.optString("serverName").ifBlank{"—"}}",
    "Транспорт: ${row.optString("transport").ifBlank{"TCP"}}",
    when(row.optString("role")){"primary"->"Роль: основной";"reserve"->"Роль: резерв";else->""},
    "VLESS: ${health(a.vless)} · ${probeLabel(row.optString("outcome"))}",
    "HA: ${health(a.ha)}"+if(a.ha==ProbeHealth.FAILED)" · ${probeLabel(row.optString("relayOutcome"))}"else"",
    probeStageSummary(row),
    "Проверка: ${row.optLong("totalMs")} мс · успешных echo ${row.optInt("echoSucceeded")}/3",
    "RTT: "+if(row.has("echoRttMs")&&!row.isNull("echoRttMs"))"${row.optLong("echoRttMs")} мс"else"—",
    "Время: ${date(row.optString("measuredAt"))}"
   ).filter{it.isNotBlank()}.joinToString("\n\n")
  }
  android.app.AlertDialog.Builder(this).setTitle(row.optString("name")).setView(ScrollView(this).apply{addView(detail)}).setPositiveButton("Закрыть",null).show()
 }
 override fun onSaveInstanceState(out:Bundle){
  out.putString("source",sources.getOrNull(choose.selectedItemPosition)?.id);out.putString("network",selectedNetwork)
  out.putInt("filter",filter.selectedItemPosition);out.putInt("sort",sort.selectedItemPosition);out.putInt("scroll",scroll.scrollY);super.onSaveInstanceState(out)
 }
 override fun onResume(){super.onResume();if(preview==null){VlessProbeCoordinator.foreground=true;handler.post(refresh)}else render()}
 override fun onPause(){handler.removeCallbacks(refresh);if(preview==null&&!isChangingConfigurations)VlessProbeCoordinator.foreground=false;super.onPause()}
 override fun onDestroy(){worker.shutdown();super.onDestroy()}
}

internal fun probeStageSummary(row:JSONObject):String {
 if(!row.has("dnsOutcome"))return "Этапы: нет данных — повторите проверку"
 fun status(key:String)=when(val v=row.optString(key)){""->"—";"confirmed"->"подтверждено HTTPS";"unconfirmed"->"не подтверждено";"not_attempted"->"не проверялось";"unknown"->"нет данных";"pending"->"ожидание";else->probeLabel(v)}
 fun phase(key:String)=when(row.optString(key)){"proxy_or_target_tls"->" · прокси / TLS цели";"http_response"->" · ожидание HTTP";"http_body"->" · HTTP-ответ";else->""}
 return "DNS: "+status("dnsOutcome")+" · TCP: "+status("tcpOutcome")+"\nTLS/REALITY прокси: "+status("proxyOutcome")+"\nHTTPS цели: "+status("httpsOutcome")+phase("httpsStage")+"\nIP выхода прокси (ipify): "+row.optString("exitIp").ifBlank{"—"}+" · "+status("exitIpOutcome")+phase("exitIpStage")
}
