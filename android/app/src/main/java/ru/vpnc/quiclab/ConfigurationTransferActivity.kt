package ru.vpnc.quiclab

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.os.Bundle
import android.text.InputType
import android.widget.*
import org.json.JSONObject
import org.json.JSONArray
import java.util.UUID
import java.util.concurrent.Executors

class ConfigurationTransferActivity:Activity(){
 private lateinit var ui:ClientUi
 private lateinit var status:TextView
 private val worker=Executors.newSingleThreadExecutor()
 private var busy=false
 private var output:ByteArray?=null
 private var input:ByteArray?=null
 private val sections=linkedMapOf("profiles" to "Подключения","routing" to "Маршруты и приложения","network" to "Сети, DNS и лимит","diagnostics" to "Диагностика")
 private val checked=linkedMapOf<String,CheckBox>()
 private lateinit var secrets:CheckBox
 private var selectedProfiles=listOf<String>()
 override fun onCreate(saved:Bundle?){
  super.onCreate(saved);ui=ClientUi(this)
  val root=ui.column();root.addView(ui.button("Назад"){finish()});root.addView(ui.text("Перенос настроек",22f,true))
  val body=ui.column();val scroll=ScrollView(this);scroll.addView(body);root.addView(scroll,LinearLayout.LayoutParams(-1,0,1f))
  val card=ui.card(body)
  card.addView(ui.text("Экспортируйте весь конфиг или выбранные разделы. Привязка MDM, согласия и счётчики трафика не переносятся."))
  sections.forEach{(key,label)->val box=CheckBox(this).apply{text=label;isChecked=true};checked[key]=box;card.addView(box)}
  ui.add(card,ui.button("Выбрать подключения"){chooseProfiles()})
  secrets=CheckBox(this).apply{text="Включить ключи (с паролем)"};card.addView(secrets)
  ui.add(card,ui.button("Экспортировать"){if(!busy)export()})
  ui.add(card,ui.button("Импортировать из файла",true){if(!busy)startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT).setType("*/*").addCategory(Intent.CATEGORY_OPENABLE),1)})
  ui.add(card,ui.button("Отменить последний импорт"){if(!busy)background("Готовим откат…",{ConfigurationTransfer.rollback(this)}){confirm(it)}})
  status=ui.text("");card.addView(status)
  if(MdmConfiguration.managed(this)){
   card.addView(ui.text("Настройками управляет MDM. Для локального импорта приостановите управление или используйте WEB-редактор MDM."))
   ui.add(card,ui.button("Управление MDM"){startActivity(Intent(this,MdmEnrollActivity::class.java))})
  }
  ui.install(root)
 }
 private fun chooseProfiles(){
  val profiles=VpnProfiles.list(this);val checks=BooleanArray(profiles.size){selectedProfiles.isEmpty()||profiles[it].id in selectedProfiles}
  AlertDialog.Builder(this).setTitle("Подключения для экспорта").setMultiChoiceItems(profiles.map{it.name}.toTypedArray(),checks){_,i,b->checks[i]=b}
   .setNegativeButton("Отмена",null).setPositiveButton("Выбрать"){_,_->
    val ids=profiles.indices.filter{checks[it]}.map{profiles[it].id}
    if(ids.isEmpty()){message("Выберите хотя бы одно подключение")}else{selectedProfiles=ids;status.text="Выбрано подключений: ${ids.size}"}
   }.show()
 }
 private fun password(title:String,action:(String)->Unit){
  val box=EditText(this).apply{inputType=InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD;isSaveEnabled=false;hint="Пароль"}
  AlertDialog.Builder(this).setTitle(title).setView(box).setNegativeButton("Отмена"){_,_->box.text.clear()}
   .setPositiveButton("Продолжить"){_,_->val value=box.text.toString();box.text.clear();action(value)}.show()
 }
 private fun export(){
  val selected=checked.filterValues{it.isChecked}.keys.toList();val keys=secrets.isChecked
  val profiles=if("profiles" in selected)selectedProfiles else emptyList()
  fun run(pass:String)=background("Готовим экспорт…",{ConfigurationTransfer.export(this,selected,profiles,keys,pass)}){bytes->
   output?.fill(0);output=bytes
   startActivityForResult(Intent(Intent.ACTION_CREATE_DOCUMENT).setType(if(keys)"application/octet-stream" else "application/json")
    .addCategory(Intent.CATEGORY_OPENABLE).putExtra(Intent.EXTRA_TITLE,if(keys)"quic-lab-config.age" else "quic-lab-config.json"),2)
  }
  if(keys)AlertDialog.Builder(this).setTitle("Экспорт ключей").setMessage("Копия даёт тот же VPN-доступ. Одновременное использование особенно ограничено для AmneziaWG; новая регистрация устройства не создаётся.")
   .setNegativeButton("Отмена",null).setPositiveButton("Задать пароль"){_,_->password("Пароль для файла, минимум 8 символов",::run)}.show()else run("")
 }
 @Deprecated("Activity result") override fun onActivityResult(request:Int,result:Int,data:Intent?){
  super.onActivityResult(request,result,data)
  val uri=data?.data
  if(result!=RESULT_OK||uri==null){if(request==2){output?.fill(0);output=null};return}
  if(request==2){val bytes=output?:return;output=null;background("Сохраняем файл…",{
   try{contentResolver.openOutputStream(uri,"wt")!!.use{it.write(bytes)}}finally{bytes.fill(0)}
  }){status.text="Файл сохранён"}}
  if(request==1)background("Читаем файл…",{
   contentResolver.openInputStream(uri)!!.use{stream->val out=java.io.ByteArrayOutputStream();val chunk=ByteArray(8192)
    while(true){val n=stream.read(chunk);if(n<0)break;require(out.size()+n<=2*1024*1024){"Файл слишком большой"};out.write(chunk,0,n)};out.toByteArray()}
  }){bytes->input?.fill(0);input=bytes
   fun decode(pass:String)=background("Проверяем файл…",{ConfigurationTransfer.decode(bytes,pass)}){raw->bytes.fill(0);input=null;chooseMode(raw)}
   if(bytes.take(22).toByteArray().toString(Charsets.US_ASCII).startsWith("age-encryption.org/v1"))password("Пароль файла",::decode)else decode("")
  }
 }
 private fun chooseMode(raw:String){
  AlertDialog.Builder(this).setTitle("Как импортировать?").setItems(arrayOf("Добавить подключения","Обновить выбранные подключения","Заменить весь конфиг")){_,i->
   val mode=listOf("add","update","replace")[i]
   if(mode=="replace")AlertDialog.Builder(this).setTitle("Заменить все настройки?").setMessage("Текущие подключения будут заменены. Перед этим можно сохранить их экспортом. Предпросмотр появится до применения.")
    .setNegativeButton("Отмена",null).setPositiveButton("Предпросмотр"){_,_->mapProfiles(raw,mode)}.show()else mapProfiles(raw,mode)
  }.setNegativeButton("Отмена",null).show()
 }
 private fun mapProfiles(raw:String,mode:String){
  try{
   val payload=JSONObject(raw).getJSONObject("payload");val profiles=payload.optJSONArray("profiles")?:JSONArray()
   val names=linkedMapOf<String,String>();for(i in 0 until profiles.length()){val p=profiles.getJSONObject(i);names[p.getString("id")]=p.getString("name")}
   val imported=names.keys.toSet()
   payload.optJSONObject("routing")?.let{r->names.putIfAbsent(r.getString("currentProfileId"),r.getString("currentProfileId"));r.getJSONObject("settings").keys().forEach{names.putIfAbsent(it,it)};val ids=r.getJSONArray("enabledProfileIds");for(i in 0 until ids.length())names.putIfAbsent(ids.getString(i),ids.getString(i))}
   payload.optJSONObject("network")?.getJSONObject("dns")?.getString("profileId")?.let{names.putIfAbsent(it,it)}
   val mapping=linkedMapOf<String,String>();val entries=names.entries.toList()
   fun step(i:Int){
    if(i==entries.size){preview(raw,mode,mapping);return}
    val e=entries[i]
    if(mode!="update"&&e.key in imported){mapping[e.key]=UUID.randomUUID().toString();step(i+1);return}
    val targets=VpnProfiles.list(this).filter{it.id !in mapping.values}
    if(targets.isEmpty()){message("Не осталось подключений для сопоставления. Используйте добавление или выберите меньше исходных профилей.");return}
    AlertDialog.Builder(this).setTitle("Куда импортировать: ${e.value}").setItems(targets.map{it.name+" · "+it.id.take(8)}.toTypedArray()){_,index->mapping[e.key]=targets[index].id;step(i+1)}.setNegativeButton("Отмена",null).show()
   };step(0)
  }catch(e:Exception){message(e.message?:"Некорректный файл")}
 }
 private fun preview(raw:String,mode:String,mapping:Map<String,String>)=background("Готовим предпросмотр…",{ConfigurationTransfer.prepare(this,raw,mode,mapping)}){p->
  confirm(p)
 }
 private fun confirm(p:TransferPreview){
  if(p.missingApps.isNotEmpty()){message(p.summary);return}
  AlertDialog.Builder(this).setTitle("Изменения конфигурации").setMessage(p.summary+"\n\nРаботающий VPN будет переподключён. Новые регистрации не создаются.")
   .setNegativeButton("Отмена",null).setPositiveButton("Применить"){_,_->background("Применяем…",{LocalConfigurationApply.apply(this,p)}){status.text="Настройки применены"}}.show()
 }
 private fun message(text:String){AlertDialog.Builder(this).setTitle("Перенос настроек").setMessage(text).setPositiveButton("Понятно",null).show()}
 private fun <T> background(label:String,work:()->T,done:(T)->Unit){if(busy)return;busy=true;status.text=label;worker.execute{
  val result=runCatching(work);runOnUiThread{busy=false;if(isDestroyed)return@runOnUiThread;result.fold({done(it)},{status.text="Операция не выполнена";message("Не удалось обработать конфиг. Проверьте файл, пароль, права MDM и актуальность настроек.")})}
 }}
 override fun onDestroy(){output?.fill(0);input?.fill(0);worker.shutdown();super.onDestroy()}
}
