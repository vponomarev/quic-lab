package ru.vpnc.quiclab

import android.annotation.SuppressLint
import android.app.Activity
import android.app.AlertDialog
import android.os.Bundle
import android.webkit.*
import android.widget.LinearLayout
import org.json.JSONObject
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean

/** Only two APK assets can execute script. No remote content, cookies or generic URL bridge. */
class MdmUserEditorActivity:Activity(){
 private lateinit var web:WebView
 private lateinit var session:MdmUserEditorSession
 private val worker=Executors.newSingleThreadExecutor()
 private val closed=AtomicBoolean(false)
 private val busy=AtomicBoolean(false)
 private val transferLock=Any()
 private var transfer:MdmTransport?=null
 @SuppressLint("SetJavaScriptEnabled","AddJavascriptInterface")
 override fun onCreate(savedInstanceState:Bundle?){
  super.onCreate(savedInstanceState)
  window.addFlags(android.view.WindowManager.LayoutParams.FLAG_SECURE)
  val app=applicationContext;val store=MdmStore(app)
  session=MdmUserEditorSession(store::read,store::secret,{binding,op,body,secret->
   val channel=MdmTransport(app,{secret})
   try{
    synchronized(transferLock){check(!closed.get());transfer=channel}
    channel.post(binding,op,body.toString())
   }finally{channel.close();synchronized(transferLock){if(transfer===channel)transfer=null}}
  },{MdmBackground.configurationSubmitted(app)})
  val ui=ClientUi(this);val root=ui.column();root.addView(ui.button("Назад"){leave()})
  web=WebView(this)
  web.settings.apply{
   javaScriptEnabled=true;allowFileAccess=false;allowContentAccess=false
   blockNetworkLoads=true;domStorageEnabled=false;cacheMode=WebSettings.LOAD_NO_CACHE
   mixedContentMode=WebSettings.MIXED_CONTENT_NEVER_ALLOW;setSupportMultipleWindows(false)
  }
  CookieManager.getInstance().setAcceptThirdPartyCookies(web,false)
  web.webViewClient=object:WebViewClient(){
   override fun shouldOverrideUrlLoading(view:WebView,request:WebResourceRequest)=true
   override fun shouldInterceptRequest(view:WebView,request:WebResourceRequest):WebResourceResponse{
    val path=if(request.method=="GET")MdmUserEditorAssets.path(request.url.toString()) else null
    return if(path==null)WebResourceResponse("text/plain","UTF-8",403,"Forbidden",emptyMap(),"".byteInputStream())
    else WebResourceResponse(if(path.endsWith(".js"))"text/javascript" else "text/html","UTF-8",assets.open(path))
   }
  }
  web.webChromeClient=object:WebChromeClient(){
   override fun onJsConfirm(view:WebView,url:String,message:String,result:JsResult):Boolean{
    AlertDialog.Builder(this@MdmUserEditorActivity).setMessage(message).setPositiveButton("Да"){_,_->result.confirm()}.setNegativeButton("Отмена"){_,_->result.cancel()}.setOnCancelListener{result.cancel()}.show();return true
   }
  }
  web.addJavascriptInterface(object{
   @JavascriptInterface fun request(id:String,operation:String,body:String){
    if(closed.get() || !id.matches(Regex("[0-9]{1,9}")))return
    if(!busy.compareAndSet(false,true)){respond(id,JSONObject().put("ok",false).put("error","Дождитесь завершения текущего запроса."));return}
    try{worker.execute{
     val result=try{JSONObject().put("ok",true).put("value",session.request(operation,body))}
      catch(e:Exception){JSONObject().put("ok",false).put("error",MdmUserEditorAssets.error(e))}
     finally{busy.set(false)}
     respond(id,result)
    }}catch(_:java.util.concurrent.RejectedExecutionException){busy.set(false)}
   }
  },"MdmNative")
  root.addView(web,LinearLayout.LayoutParams(-1,0,1f));ui.install(root)
  web.loadUrl(MdmUserEditorAssets.URL)
 }
 private fun respond(id:String,result:JSONObject){runOnUiThread{if(!closed.get())web.evaluateJavascript("window.mdmEditorResult(${JSONObject.quote(id)},${JSONObject.quote(result.toString())})",null)}}
 private fun leave(){
  if(busy.get()){AlertDialog.Builder(this).setMessage("Запрос ещё выполняется. При закрытии сервер мог уже сохранить изменения.").setPositiveButton("Закрыть"){_,_->finish()}.setNegativeButton("Подождать",null).show();return}
  web.evaluateJavascript("Boolean(window.mdmEditorDirty && window.mdmEditorDirty())"){dirty->
   if(isFinishing)return@evaluateJavascript
   if(dirty=="true")AlertDialog.Builder(this).setTitle("Отбросить черновик?").setMessage("Несохранённые изменения будут потеряны.").setPositiveButton("Отбросить"){_,_->finish()}.setNegativeButton("Продолжить",null).show() else finish()
  }
 }
 @Deprecated("Legacy back dispatch") override fun onBackPressed(){leave()}
 override fun onDestroy(){
  closed.set(true);if(::session.isInitialized)session.close()
  synchronized(transferLock){transfer?.close()};worker.shutdownNow()
  if(::web.isInitialized){web.removeJavascriptInterface("MdmNative");web.stopLoading();web.destroy()}
  super.onDestroy()
 }
}
