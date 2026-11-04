package ru.vpnc.quiclab

import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import android.util.Base64
import androidx.test.platform.app.InstrumentationRegistry
import mobile.Mobile
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.io.File
import java.util.UUID

class MdmUserEditorWireTest {
 private class Isolated(base:Context):ContextWrapper(base){
  val prefix="mdm-user-wire-${UUID.randomUUID()}";val names=mutableSetOf<String>();val dir=File(base.cacheDir,prefix).apply{check(mkdirs())}
  override fun getApplicationContext():Context=this
  override fun getFilesDir()=dir
  override fun getNoBackupFilesDir()=File(dir,"private").apply{mkdirs()}
  override fun getSharedPreferences(name:String,mode:Int):SharedPreferences{names.add(prefix+name);return baseContext.getSharedPreferences(prefix+name,mode)}
  fun close(){names.forEach{baseContext.deleteSharedPreferences(it)};check(dir.parentFile==baseContext.cacheDir);dir.deleteRecursively()}
 }
 @Test fun importSaveRetryApplyAndRevokeOverRealHTTPS(){
  val inst=InstrumentationRegistry.getInstrumentation();val args=InstrumentationRegistry.getArguments()
  val endpoint=args.getString("mdm_endpoint").orEmpty();assumeTrue("Explicit isolated Linux fixture required",endpoint.isNotEmpty())
  assumeTrue("Test never interrupts an active VPN",!LabVpnService.active)
  val ca=String(Base64.decode(args.getString("mdm_ca"),Base64.DEFAULT),Charsets.UTF_8)
  val c=Isolated(inst.targetContext)
  val certificate=java.security.cert.CertificateFactory.getInstance("X.509").generateCertificate(ca.byteInputStream())
  val trust=java.security.KeyStore.getInstance(java.security.KeyStore.getDefaultType()).apply{load(null);setCertificateEntry("fixture",certificate)}
  val tm=javax.net.ssl.TrustManagerFactory.getInstance(javax.net.ssl.TrustManagerFactory.getDefaultAlgorithm()).apply{init(trust)}
  val ssl=javax.net.ssl.SSLContext.getInstance("TLS").apply{init(null,tm.trustManagers,null)}
  fun admin(path:String,id:String,enabled:Boolean=false):JSONObject{
   val connection=java.net.URL(endpoint+"/fixture/"+path).openConnection() as javax.net.ssl.HttpsURLConnection
   try{
    connection.sslSocketFactory=ssl.socketFactory;connection.instanceFollowRedirects=false;connection.connectTimeout=10000;connection.readTimeout=10000
    connection.requestMethod="POST";connection.doOutput=true;connection.setRequestProperty("Authorization","Bearer "+args.getString("mdm_admin"))
    connection.outputStream.use{it.write(JSONObject().put("id",id).put("enabled",enabled).toString().toByteArray())}
    assertEquals(200,connection.responseCode);return JSONObject(connection.inputStream.bufferedReader().use{it.readText()})
   }finally{connection.disconnect()}
  }
  try{
   VpnProfiles.list(c);val store=MdmStore(c)
   val controller=MdmController(store,{object:MdmGateway{
    var transport:MdmTransport?=null
    override fun post(b:MdmBinding,op:String,body:JSONObject,secret:String):JSONObject{
     if(op=="sync")body.put("wait",false)
     return MdmTransport(c,{secret},ca).use{transport=it;JSONObject(it.post(b,op,body.toString()))}
    }
    override fun close(){transport?.close()}
   }},{MdmConfiguration.detach(c)})
   val rights=MdmRights(config=true)
   controller.enroll(MdmInvitation(endpoint,args.getString("mdm_token")!!,rights),rights)
   repeat(2){assertTrue(controller.poll{_,_->})}
   assertTrue(controller.report{s->MdmDeviceReport.snapshot(c,s)})
   var kicks=0
   val editor=MdmUserEditorSession(store::read,store::secret,{b,op,body,secret->MdmTransport(c,{secret},ca).use{it.post(b,op,body.toString())}},{kicks++})
   assertFalse(editor.request("read","{}").getBoolean("editable"))
   val id=store.read().binding!!.id;admin("permission",id,true)
   val view=editor.request("read","{}");assertTrue(view.getBoolean("editable"))
   val before=MdmConfiguration.snapshotLocal(c).toString()
   val doc=view.getJSONObject("report").getJSONObject("configuration")
   val source=JSONObject(doc.toString());val profile=source.getJSONArray("profiles").getJSONObject(0)
   val profileId=profile.getString("id");profile.put("name","Imported over HTTPS")
   profile.getJSONObject("settings").put("transport","vless")
   profile.put("identity",JSONObject().put("vless_uri","vless://11111111-1111-4111-8111-111111111111@example.org:443?security=tls&type=tcp&sni=example.org"))
   val bundle=Mobile.exportConfiguration(source.toString(),"""{"sections":["profiles","routing","network","diagnostics"]}""",true)
   val encrypted=Mobile.encryptConfiguration(bundle.toByteArray(),"wire-password")
   val imported=MdmUserImport.prepare(doc,MdmUserImport.decode(encrypted,"wire-password"),"update",mapOf(profileId to profileId)).getJSONObject("candidate")
   assertEquals(before,MdmConfiguration.snapshotLocal(c).toString())
   val request=JSONObject().put("policyRevision",view.getJSONObject("userConfigEdit").getLong("revision")).put("expectedRevision",0)
    .put("expectedGeneration",view.getJSONObject("report").getLong("configGeneration")).put("requestId",UUID.randomUUID().toString()).put("document",imported)
   val revision=editor.request("save",request.toString()).getLong("revision")
   assertEquals(revision,editor.request("save",request.toString()).getLong("revision"));assertEquals(2,kicks)
   assertEquals(before,MdmConfiguration.snapshotLocal(c).toString())
   assertFalse(editor.request("read","{}").toString().contains("11111111-1111-4111-8111-111111111111"))
   var desired:MdmConfigRevision?=null;assertTrue(controller.poll{_,r->desired=r.desiredConfig});assertNotNull(desired)
   val port=object:MdmVpnPort{override fun active()=false;override fun stop(){};override fun start()="stopped"}
   val applied=MdmApplyCoordinator(c,port).apply(store.read(),desired!!);assertTrue(applied.errorCode,applied.configurationApplied)
   assertEquals("Imported over HTTPS",VpnProfiles.current(c).name)
   assertTrue(controller.report{s->MdmDeviceReport.snapshot(c,s)})
   assertTrue(controller.poll{_,r->assertNull(r.desiredConfig)})
   assertEquals(revision,store.read().appliedRevision)
   admin("permission",id,false)
   // Use transport directly: applying a revision correctly invalidates the old editor session.
   val binding=store.read().binding!!
   val denied=runCatching{MdmTransport(c,store::secret,ca).use{it.post(binding,"config-submit",JSONObject(request.toString()).put("version",1).put("bindingId",id).put("epoch",binding.epoch).toString())}}
   assertTrue(denied.isFailure);assertTrue(denied.exceptionOrNull()!!.message!!.contains("401"))
   editor.close();controller.pause();controller.delete();assertTrue(admin("finish",id).getBoolean("verified"))
  }finally{c.close()}
 }
}
