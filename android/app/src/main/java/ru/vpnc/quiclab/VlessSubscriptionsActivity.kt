package ru.vpnc.quiclab

import android.app.Activity
import android.app.AlertDialog
import android.os.Bundle
import android.view.View
import android.widget.*
import java.util.concurrent.Executors

class VlessSubscriptionsActivity:Activity(){
 private val worker=Executors.newSingleThreadExecutor()
 private lateinit var body:LinearLayout
 private lateinit var status:TextView
 private var busy=false
 override fun onCreate(b:Bundle?){super.onCreate(b);render()}
 private fun button(text:String,action:()->Unit){body.addView(Button(this).apply{this.text=text;setOnClickListener{if(!busy)action()}})}
 private fun render(){
  body=LinearLayout(this).apply{orientation=LinearLayout.VERTICAL;setPadding(24,24,24,24)}
  setContentView(ScrollView(this).apply{addView(body)})
  body.addView(TextView(this).apply{text="Подписки VLESS";textSize=23f})
  body.addView(TextView(this).apply{text="HTTPS-ссылка на VLESS (текст, Base64 или JSON Xray). Обновление вручную, после просмотра изменений. Поддерживаются TCP TLS/REALITY. Исчезнувшие записи сохраняются; удалённые вручную могут вернуться при обновлении."})
  status=TextView(this);body.addView(status)
  button("Добавить подписку"){
   val fields=LinearLayout(this).apply{orientation=LinearLayout.VERTICAL}
   val name=EditText(this).apply{hint="Название подписки";setSingleLine()}
   val url=EditText(this).apply{hint="https://…";setSingleLine();isSaveEnabled=false;importantForAutofill=View.IMPORTANT_FOR_AUTOFILL_NO;inputType=android.text.InputType.TYPE_CLASS_TEXT or android.text.InputType.TYPE_TEXT_VARIATION_PASSWORD}
   fields.addView(name);fields.addView(url)
   AlertDialog.Builder(this).setTitle("Подписка VLESS").setView(fields).setNegativeButton("Отмена"){_,_->url.text.clear()}.setPositiveButton("Загрузить"){_,_->val raw=url.text.toString().trim();url.text.clear();refresh(name.text.toString().trim(),raw)}.show()
  }
  val entries=VlessSubscriptions.list(this)
  for(id in entries.keys()){
   val item=entries.getJSONObject(id)
   body.addView(TextView(this).apply{text=item.getString("name")+" · "+java.text.DateFormat.getDateTimeInstance().format(java.util.Date(item.getLong("updatedAt")))})
   button("Обновить «${item.getString("name")}»"){refresh(item.getString("name"),item.getString("url"))}
  }
  button("Назад"){finish()}
 }
 private fun refresh(name:String,url:String){
  if(MdmConfiguration.managed(this)){status.text="Настройками управляет MDM. Приостановите MDM в настройках приложения.";return}
  run("Загружаем подписку…",{VlessSubscriptions.fetch(url)}){parsed->
   val previous=VlessSubscriptions.selected(this,url)
   val checked=BooleanArray(parsed.entries.size){parsed.entries[it].key in previous}
   val labels=parsed.entries.map{it.name+"\n"+it.endpoint}.toTypedArray()
   val dialog=AlertDialog.Builder(this).setTitle("Выберите подключения (${parsed.entries.size})")
    .setMultiChoiceItems(labels,checked){_,i,on->checked[i]=on}
    .setNegativeButton("Отмена",null).setNeutralButton("Группой",null).setPositiveButton("Отдельно",null).create()
   dialog.setOnShowListener{
   for((button,asGroup) in listOf(AlertDialog.BUTTON_POSITIVE to false,AlertDialog.BUTTON_NEUTRAL to true))dialog.getButton(button).setOnClickListener{
    val chosen=parsed.entries.filterIndexed{i,_->checked[i]}
    if(chosen.isEmpty()){Toast.makeText(this,"Выберите хотя бы одно подключение",Toast.LENGTH_SHORT).show();return@setOnClickListener}
    dialog.dismiss()
    run("Готовим изменения…",{VlessSubscriptions.prepare(this,name,url,parsed.copy(entries=chosen),asGroup)}){p->
     AlertDialog.Builder(this).setTitle("Изменения подписки")
      .setMessage(p.summary+"\nВыбранные серверы импортируются отдельными подключениями или одной группой с автоматической ротацией. DNS, маршрутизация и балансировка Xray не переносятся. Общий лимит — 32 подключения.\nVPN при применении может переподключиться.")
      .setNegativeButton("Отмена",null).setPositiveButton("Применить"){_,_->run("Применяем…",{LocalConfigurationApply.apply(this,p)}){render();status.text="Подписка обновлена"}}.show()
    }
   }}
   dialog.show()
  }
 }
 private fun <T> run(label:String,work:()->T,done:(T)->Unit){if(busy)return;busy=true;status.text=label;worker.execute{val result=runCatching(work);runOnUiThread{busy=false;if(isDestroyed||isFinishing)return@runOnUiThread;result.fold(done){status.text="Не удалось обновить подписку. Проверьте HTTPS-ссылку, доступность сервера, TCP TLS/REALITY, лимит 32 подключений и права MDM. Настройки не изменены."}}}}
 override fun onDestroy(){worker.shutdownNow();super.onDestroy()}
}
