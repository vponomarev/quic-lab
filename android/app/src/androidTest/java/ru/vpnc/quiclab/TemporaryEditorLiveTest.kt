package ru.vpnc.quiclab

import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

/** Opt-in TLS fixture. All config and keys are disposable, never the phone's working profile. */
class TemporaryEditorLiveTest {
 @Test fun browserToPhoneOverBoundHttps(){
  val args=InstrumentationRegistry.getArguments();val link=args.getString("editor_link").orEmpty();assumeTrue(link.isNotEmpty())
  val inv=TemporaryEditorInvitation.parse(String(android.util.Base64.decode(link,0)))
  val ca=String(android.util.Base64.decode(args.getString("editor_ca"),0))
  val real=InstrumentationRegistry.getInstrumentation().targetContext.applicationContext
  val suffix="editor-live-${System.nanoTime()}";val dir=java.io.File(real.noBackupFilesDir,suffix).apply{mkdirs()}
  val c=object:android.content.ContextWrapper(real){
   override fun getNoBackupFilesDir()=dir
   override fun getFilesDir()=dir
   override fun getApplicationContext():android.content.Context=this
   override fun getSharedPreferences(name:String,mode:Int)=real.getSharedPreferences("$suffix-$name",mode)
  }
  var secret=inv.token
  val port=object:MdmVpnPort{override fun active()=false;override fun stop(){};override fun start():String{error("Must not start VPN")}}
  val session=TemporaryEditorRuntime.Session(c,inv,{op,body->
   val out=MdmTransport(c,{secret},ca).use{JSONObject(it.postEditor(inv.endpoint,op,body.toString()))}
   if(op=="pair")secret=out.getString("deviceToken")
   out
  },port)
  try{
   VpnProfiles.list(c)
   val uri="vless://11111111-1111-4111-8111-111111111111@example.org:443?security=tls&type=tcp&sni=example.org"
   VpnIdentity.writeBundle(c,"default",JSONObject().put("vless_config",mobile.Mobile.importVLESSConfig(uri)))
   VpnProfiles.preferences(c,"default").edit().putString("transport","vless").commit()
   session.sync()
   val deadline=android.os.SystemClock.elapsedRealtime()+90000
   while(!DiagnosticsPolicy.detailed(c)&&android.os.SystemClock.elapsedRealtime()<deadline){session.sync();Thread.sleep(1000)}
   assertTrue("Browser update was not applied",DiagnosticsPolicy.detailed(c))
   assertNull(MdmStore(c).read().binding)
   Thread.sleep(12000) // Let the browser observe the real acknowledgement.
   MdmTransport(c,{secret},ca).use{it.postEditor(inv.endpoint,"revoke",JSONObject().put("id",inv.id).toString())}
   session.close();assertFalse(session.valid())
  }finally{session.close();dir.deleteRecursively();for(n in listOf("vpn","vpn_profiles","vpn_budget","vpn_rtt","vpn_reserve","diagnostics_settings"))real.deleteSharedPreferences("$suffix-$n")}
 }
}
