package ru.vpnc.quiclab
import org.junit.Test
import org.junit.Assert.*
class TemporaryEditorTest {
 @Test fun pairingIsStrictAndCredentialsOnlyInFragment(){
  val link="quiclab://editor/pair#server=https%3A%2F%2Fexample.org&id=${"a".repeat(64)}&token=${"b".repeat(64)}&minutes=30"
  val p=TemporaryEditorInvitation.parse(link)
  assertEquals("https://example.org",p.endpoint);assertEquals(30,p.minutes)
  for(bad in listOf(link.replace("https%3A","http%3A"),link.replace("#","?"),link+"&minutes=60",link.replace("minutes=30","minutes=90"),link.replace("example.org","user%40example.org"))){assertTrue(runCatching{TemporaryEditorInvitation.parse(bad)}.isFailure)}
 }
 @Test fun localConsentDeadlineAndRevokeCannotResume(){
  var now=100L
  val lease=TemporaryEditorLease(15){now}
  assertTrue(lease.valid());now+=15*60*1000L;assertFalse(lease.valid())
  now=100;assertFalse(lease.valid())
  val next=TemporaryEditorLease(30){now};next.revoke();assertFalse(next.valid())
 }
 @Test fun sessionAppliesOnlyCurrentRevisionAndNeverStartsIdleVpn(){
  val real=androidx.test.platform.app.InstrumentationRegistry.getInstrumentation().targetContext
  val suffix="editor-test-${System.nanoTime()}";val dir=java.io.File(real.noBackupFilesDir,suffix).apply{mkdirs()}
  val c=object:android.content.ContextWrapper(real){
   override fun getNoBackupFilesDir()=dir
   override fun getFilesDir()=dir
   override fun getApplicationContext():android.content.Context=this
   override fun getSharedPreferences(name:String,mode:Int)=real.getSharedPreferences("$suffix-$name",mode)
  }
  var starts=0;val port=object:MdmVpnPort{override fun active()=false;override fun stop(){};override fun start():String{starts++;return "starting"}}
  val inv=TemporaryEditorInvitation("https://example.org","a".repeat(64),"b".repeat(64),30)
  var pending:org.json.JSONObject?=null;var acknowledgement:org.json.JSONObject?=null;var reports=0
  val session=TemporaryEditorRuntime.Session(c,inv,{op,body->when(op){
   "pair"->org.json.JSONObject().put("deviceToken","c".repeat(64))
   "poll"->org.json.JSONObject().also{pending?.let{p->it.put("pending",p)}}
   "ack"->{acknowledgement=body;pending=null;org.json.JSONObject()}
   "report"->{reports++;pending=null;org.json.JSONObject()}
   else->error(op)
  }},port)
  try{
   VpnProfiles.list(c)
   val uri="vless://11111111-1111-4111-8111-111111111111@example.org:443?security=tls&type=tcp&sni=example.org"
   VpnIdentity.writeBundle(c,"default",org.json.JSONObject().put("vless_config",mobile.Mobile.importVLESSConfig(uri)))
   VpnProfiles.preferences(c,"default").edit().putString("transport","vless").commit()
   session.sync();assertEquals(0,starts)
   val doc=MdmConfiguration.snapshotLocal(c);doc.getJSONObject("diagnostics").put("detailed",true)
   doc.getJSONArray("profiles").put(org.json.JSONObject().put("id","11111111-1111-4111-8111-111111111112").put("name","Incomplete disabled").put("settings",org.json.JSONObject()))
   pending=org.json.JSONObject().put("revision",1).put("baseGeneration",0).put("document",doc)
   session.sync();assertEquals("applied",acknowledgement!!.getString("status"));assertTrue(DiagnosticsPolicy.detailed(c));assertEquals(0,starts)
   // A local edit invalidates the older browser candidate before it can apply.
   pending=org.json.JSONObject().put("revision",2).put("baseGeneration",1).put("document",doc)
   VpnProfiles.preferences(c,"default").edit().putString("endpoint","changed.example:443").commit()
   session.sync();assertEquals(1,reports)
   session.close();assertFalse(session.valid());assertTrue(runCatching{session.sync()}.isFailure)
   assertNull(MdmStore(c).read().binding)
  }finally{session.close();dir.deleteRecursively();for(n in listOf("vpn","vpn_profiles","vpn_budget","vpn_rtt","vpn_reserve","diagnostics_settings"))real.deleteSharedPreferences("$suffix-$n")}
 }
}