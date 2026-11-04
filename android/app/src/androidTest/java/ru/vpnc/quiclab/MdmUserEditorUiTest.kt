package ru.vpnc.quiclab

import android.graphics.Bitmap
import android.graphics.Canvas
import android.webkit.*
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

class MdmUserEditorUiTest {
 @Test fun packagedFormsSaveThroughServerWithoutLocalMutation(){
  val inst=InstrumentationRegistry.getInstrumentation();val ctx=inst.targetContext
  val configuration=JSONObject("""{"schema":1,"profiles":[{"id":"default","name":"Основной","settings":{"transport":"quic","endpoint":"vpn.example:443","hostname":"vpn.example","dns":"1.1.1.1"}}],"currentProfileId":"default","enabledProfileIds":["default"],"multiple":false,"globalApps":[],"dns":{"mode":"tunnel","profileId":"default"},"budget":{"limitBytes":0},"diagnostics":{"enabled":true,"detailed":false}}""")
  val state=JSONObject().put("binding",JSONObject().put("id","fixture").put("epoch",1).put("active",true).put("grantedRights",JSONObject().put("config",true)))
   .put("userConfigEdit",JSONObject().put("enabled",true).put("revision",2)).put("editable",true)
   .put("report",JSONObject().put("configuration",configuration).put("configGeneration",9).put("appliedRevision",5).put("capabilities",org.json.JSONArray().put("web-control-v1")))
   .put("desired",JSONObject().put("revision",5).put("mode","external").put("document",configuration))
  val saved=AtomicReference<JSONObject>();val errors=mutableListOf<String>();lateinit var web:WebView
  inst.runOnMainSync{
   web=WebView(ctx);web.settings.javaScriptEnabled=true;web.settings.blockNetworkLoads=true;web.settings.allowFileAccess=false;web.settings.allowContentAccess=false
   web.webChromeClient=object:WebChromeClient(){
    override fun onJsConfirm(v:WebView,u:String,m:String,r:JsResult):Boolean{r.confirm();return true}
    override fun onConsoleMessage(m:ConsoleMessage):Boolean{if(m.messageLevel()==ConsoleMessage.MessageLevel.ERROR)errors.add(m.message());return true}
   }
   web.webViewClient=object:WebViewClient(){override fun shouldInterceptRequest(v:WebView,r:WebResourceRequest):WebResourceResponse{
    val path=MdmUserEditorAssets.path(r.url.toString())?:return WebResourceResponse("text/plain","UTF-8","".byteInputStream())
    return WebResourceResponse(if(path.endsWith("js"))"text/javascript" else "text/html","UTF-8",ctx.assets.open(path))
   }}
   web.addJavascriptInterface(object{
    @JavascriptInterface fun request(id:String,op:String,raw:String){
     val value=if(op=="read")JSONObject(state.toString()) else {saved.set(JSONObject(raw));JSONObject().put("revision",6)}
     val response=JSONObject().put("ok",true).put("value",value)
     web.post{web.evaluateJavascript("window.mdmEditorResult(${JSONObject.quote(id)},${JSONObject.quote(response.toString())})",null)}
    }
   },"MdmNative")
   web.layout(0,0,1080,1800);web.loadUrl(MdmUserEditorAssets.URL)
  }
  fun evaluate(script:String):String{val done=CountDownLatch(1);var result="";inst.runOnMainSync{web.evaluateJavascript(script){result=it;done.countDown()}};assertTrue(done.await(5,TimeUnit.SECONDS));return result}
  try{
   var ready=false
   repeat(50){if(!ready){Thread.sleep(100);ready=evaluate("Boolean(document.querySelector('#configuration input'))")=="true"}}
   assertTrue("Editor did not render: $errors",ready)
   assertEquals("false",evaluate("document.querySelector('#apply').disabled"))
   assertEquals("false",evaluate("document.body.innerText.includes('Разрешить пользователю выбирать подключение')"))
   evaluate("document.querySelector('[data-tab=config]').click();var label=Array.from(document.querySelectorAll('#configuration label')).find(x=>x.textContent==='Название подключения');var input=label.querySelector('input');input.value='Изменено на телефоне';input.dispatchEvent(new Event('change'));true")
   assertEquals("true",evaluate("window.mdmEditorDirty()"))
   inst.runOnMainSync{
    val image=Bitmap.createBitmap(1080,1800,Bitmap.Config.ARGB_8888);web.draw(Canvas(image))
    File(ctx.getExternalFilesDir(null),"mdm-user-editor-preview.png").outputStream().use{image.compress(Bitmap.CompressFormat.PNG,100,it)};image.recycle()
   }
   evaluate("document.querySelector('#apply').click();true")
   repeat(50){if(saved.get()==null)Thread.sleep(100)}
   val request=requireNotNull(saved.get()){"No save request: $errors"}
   assertEquals(2,request.getInt("policyRevision"));assertEquals(5,request.getInt("expectedRevision"));assertEquals(9,request.getInt("expectedGeneration"))
   assertEquals("Изменено на телефоне",request.getJSONObject("document").getJSONArray("profiles").getJSONObject(0).getString("name"))
   assertEquals("Основной",configuration.getJSONArray("profiles").getJSONObject(0).getString("name"))
   assertFalse(request.has("bindingId"));assertFalse(request.has("mode"))
   assertEquals("true",evaluate("document.querySelector('#apply').disabled"))
   assertEquals("true",evaluate("document.querySelector('#status').textContent.includes('Ожидаем применения')"))
   assertTrue("JS errors: $errors",errors.isEmpty())
  }finally{inst.runOnMainSync{web.removeJavascriptInterface("MdmNative");web.destroy()}}
 }
}
