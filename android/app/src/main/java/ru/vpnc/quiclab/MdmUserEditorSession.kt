package ru.vpnc.quiclab

import org.json.JSONObject
import java.io.Closeable
import java.net.URI
import java.util.concurrent.atomic.AtomicBoolean

/** A device-specific control session. It never writes preferences or applies a document. */
internal class MdmUserEditorSession(
 private val state:()->MdmState,
 private val secret:()->String,
 private val exchange:(MdmBinding,String,JSONObject,String)->String,
 private val submitted:()->Unit,
):Closeable {
 private val closed=AtomicBoolean(false)
 private val initial=state()
 private val binding=initial.binding
 internal fun checkCurrent(){
  val now=state()
  check(!closed.get() && initial.active && initial.rights.config && binding!=null &&
   now.active && now.rights.config && !now.cleanupPending && now.localGeneration==initial.localGeneration &&
   now.binding?.id==binding.id && now.binding.epoch==binding.epoch){"Привязка MDM изменилась. Откройте редактор заново."}
 }
 fun request(operation:String,raw:String):JSONObject {
  checkCurrent()
  require(raw.toByteArray(Charsets.UTF_8).size<=1048576)
  val draft=JSONObject(raw)
  val expected=when(operation){"read"->emptySet();"save"->setOf("policyRevision","expectedRevision","expectedGeneration","requestId","document");else->error("Недоступная операция")}
  require(draft.keys().asSequence().toSet()==expected)
  val body=JSONObject().put("version",1).put("bindingId",binding!!.id).put("epoch",binding.epoch)
  if(operation=="save"){
   for(key in listOf("policyRevision","expectedRevision","expectedGeneration")){val n=draft.getLong(key);require(n>=0);body.put(key,n)}
   val request=draft.getString("requestId");require(request.length in 1..96);body.put("requestId",request).put("document",draft.getJSONObject("document"))
  }
  val response=exchange(binding,if(operation=="read")"config-editor" else "config-submit",body,secret())
  checkCurrent()
  require(response.toByteArray(Charsets.UTF_8).size<=if(operation=="read")3*1048576 else 1048576)
  val result=JSONObject(response)
  if(operation=="save"){
   require(result.getLong("revision")>0)
   submitted()
  }else{
   val owner=result.getJSONObject("binding")
   require(owner.getString("id")==binding.id && owner.getLong("epoch")==binding.epoch)
  }
  return result
 }
 override fun close(){closed.set(true)}
}

internal object MdmUserEditorAssets {
 const val URL="https://mdm-editor.invalid/index.html"
 fun path(value:String):String?=runCatching{
  val u=URI(value)
  if(u.scheme!="https" || u.host!="mdm-editor.invalid" || u.port!=-1 || u.rawUserInfo!=null || u.rawQuery!=null || u.rawFragment!=null)return null
  when(u.rawPath){"/index.html"->"mdm/mdm_phone.html";"/mdm_editor.js"->"mdm/mdm_editor.js";else->null}
 }.getOrNull()
 fun error(error:Throwable):String=when {
  error.message?.contains("MDM HTTP status 409")==true->"Настройки или права изменились. Перечитайте состояние; черновик не отправлен повторно."
  error.message?.contains("MDM HTTP status 401")==true || error.message?.contains("MDM HTTP status 403")==true->"Сервер не разрешил редактирование. Проверьте право у администратора."
  error.message?.contains("MDM HTTP status 404")==true->"Сервер не поддерживает редактирование с телефона. Обновите MDM-сервер."
  error.message?.contains("MDM HTTP status 400")==true->"Сервер отклонил настройки. Проверьте адреса, маршруты и данные доступа."
  else->"Не удалось подтвердить операцию. Проверьте связь и состояние MDM. Черновик остаётся в редакторе; сервер мог сохранить изменения."
 }
}
