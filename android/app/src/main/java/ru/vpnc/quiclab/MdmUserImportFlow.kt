package ru.vpnc.quiclab

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.text.InputType
import android.widget.EditText
import org.json.JSONObject
import java.util.UUID
import java.util.concurrent.ExecutorService

/** One explicit SAF selection; all results go back to the WebView draft. */
internal class MdmUserImportFlow(
 private val activity:Activity,private val worker:ExecutorService,
 private val current:()->Unit,private val complete:(JSONObject?,String?)->Unit,
) {
 private var closed=false
 private var bytes:ByteArray?=null
 private var draft:JSONObject?=null
 private var bundle:String?=null
 private var dialog:AlertDialog?=null
 private fun alive()=!closed&&!activity.isFinishing&&!activity.isDestroyed
 fun start(body:String){
  try{
   current();require(body.toByteArray(Charsets.UTF_8).size<=1048576)
   val input=JSONObject(body);require(input.keys().asSequence().toSet()==setOf("document"))
   draft=JSONObject(input.getJSONObject("document").toString())
   activity.startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT).setType("*/*").addCategory(Intent.CATEGORY_OPENABLE),REQUEST)
  }catch(_:Exception){fail()}
 }
 fun result(code:Int,data:Intent?){
  if(!alive())return
  val uri=data?.data
  if(code!=Activity.RESULT_OK||uri==null){cancel();return}
  background({require(uri.scheme=="content");activity.contentResolver.openInputStream(uri)!!.use(MdmUserImport::read)}){value->
   bytes=value
   if(value.take(22).toByteArray().toString(Charsets.US_ASCII).startsWith("age-encryption.org/v1"))password() else decode("")
  }
 }
 private fun password(){
  val input=EditText(activity).apply{inputType=InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD;isSaveEnabled=false;importantForAutofill=android.view.View.IMPORTANT_FOR_AUTOFILL_NO_EXCLUDE_DESCENDANTS;hint="Пароль файла"}
  dialog=AlertDialog.Builder(activity).setTitle("Пароль зашифрованного конфига").setView(input)
   .setNegativeButton("Отмена"){_,_->input.text.clear();cancel()}
   .setPositiveButton("Продолжить"){_,_->val pass=input.text.toString();input.text.clear();decode(pass)}
   .setOnCancelListener{input.text.clear();cancel()}.create().also{
    it.window?.addFlags(android.view.WindowManager.LayoutParams.FLAG_SECURE)
    it.setOnDismissListener{input.text.clear()};it.show()
   }
 }
 private fun decode(password:String){
  val input=bytes?:return
  background({try{MdmUserImport.decode(input,password)}finally{input.fill(0)}}){raw->bytes=null;bundle=raw;chooseMode()}
 }
 private fun chooseMode(){
  dialog=AlertDialog.Builder(activity).setTitle("Импортировать в MDM-черновик")
   .setItems(arrayOf("Добавить подключения","Обновить выбранные подключения","Заменить весь конфиг")){_,i->
    val mode=listOf("add","update","replace")[i]
    if(mode=="replace")dialog=AlertDialog.Builder(activity).setTitle("Заменить конфигурацию в черновике?")
     .setMessage("Подключения будут заменены в черновике. Отправка на сервер выполняется отдельно кнопкой «Отправить».")
     .setPositiveButton("Подготовить"){_,_->mapProfiles(mode)}.setNegativeButton("Отмена"){_,_->cancel()}.setOnCancelListener{cancel()}.show()
    else mapProfiles(mode)
   }.setNegativeButton("Отмена"){_,_->cancel()}.setOnCancelListener{cancel()}.show()
 }
 private fun mapProfiles(mode:String){
  try{
   current()
   val payload=JSONObject(bundle!!).getJSONObject("payload")
   val profiles=payload.optJSONArray("profiles")
   require(profiles==null||profiles.length()<=32)
   val imported=if(profiles==null)emptySet() else (0 until profiles.length()).map{profiles.getJSONObject(it).getString("id")}.toSet()
   val names=ConfigurationReferences.names(payload).entries.toList();require(names.size<=128)
   val mapping=linkedMapOf<String,String>()
   val existing=draft!!.getJSONArray("profiles");require(existing.length()<=32)
   fun step(index:Int){
    if(!alive())return
    try{
     current()
     if(index==names.size){prepare(mode,mapping);return}
     val entry=names[index]
     if(mode!="update"&&entry.key in imported){mapping[entry.key]=UUID.randomUUID().toString();step(index+1);return}
     val choices=(0 until existing.length()).map{existing.getJSONObject(it)}.filter{it.getString("id") !in mapping.values}
     require(choices.isNotEmpty())
     dialog=AlertDialog.Builder(activity).setTitle("Куда импортировать: ${entry.value.take(100)}")
      .setItems(choices.map{it.getString("name").take(100)+" · "+it.getString("id").take(8)}.toTypedArray()){_,selected->mapping[entry.key]=choices[selected].getString("id");step(index+1)}
      .setNegativeButton("Отмена"){_,_->cancel()}.setOnCancelListener{cancel()}.show()
    }catch(_:Exception){fail()}
   }
   step(0)
  }catch(_:Exception){fail()}
 }
 private fun prepare(mode:String,mapping:Map<String,String>){
  val before=draft?:return;val raw=bundle?:return
  background({MdmUserImport.prepare(before,raw,mode,mapping)}){finish(it,null)}
 }
 private fun <T> background(work:()->T,done:(T)->Unit){
  if(!alive())return
  try{worker.execute{
   val result=runCatching{current();work()}
   activity.runOnUiThread{
    if(!alive()){(result.getOrNull() as? ByteArray)?.fill(0);return@runOnUiThread}
    try{current();result.fold(done,{fail()})}catch(_:Exception){(result.getOrNull() as? ByteArray)?.fill(0);fail()}
   }
  }}catch(_:Exception){fail()}
 }
 private fun cancel()=finish(null,"Импорт отменён. Черновик не изменён.")
 private fun fail()=finish(null,"Не удалось импортировать. Проверьте формат, пароль, сопоставление профилей и состояние MDM. Черновик не изменён.")
 private fun finish(value:JSONObject?,error:String?){if(!alive())return;close();complete(value,error)}
 fun close(){closed=true;bytes?.fill(0);bytes=null;draft=null;bundle=null;dialog?.dismiss();dialog=null}
 companion object{const val REQUEST=0x4d49}
}
