package ru.vpnc.quiclab
import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import android.util.Base64
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.UUID
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
class MdmWireTest {
 private class Isolated(base:Context):ContextWrapper(base){
  val prefix="mdm-wire-"+UUID.randomUUID();val names=mutableSetOf<String>();val dir=File(base.cacheDir,prefix).apply{mkdirs()}
  override fun getFilesDir()=dir
  override fun getNoBackupFilesDir()=File(dir,"private").apply{mkdirs()}
  override fun getSharedPreferences(name:String,mode:Int):SharedPreferences{names.add(prefix+name);return baseContext.getSharedPreferences(prefix+name,mode)}
  fun close(){names.forEach{baseContext.deleteSharedPreferences(it)};dir.deleteRecursively()}
 }
 @Test fun connectedPhoneStatus(){
  val inst=InstrumentationRegistry.getInstrumentation();val state=MdmStore(inst.targetContext).read()
  val out=android.os.Bundle();out.putString("mdm_status",JSONObject().put("active",state.active).put("rights",state.rights.json()).put("bindingId",state.binding?.id).put("endpoint",state.binding?.endpoint).put("vpnRunning",LabVpnService.active).toString());inst.sendStatus(0,out)
 }
 @Test fun reportApplyAndAckOverRealHTTPS(){
  val args=InstrumentationRegistry.getArguments();val endpoint=args.getString("mdm_endpoint").orEmpty()
  assumeTrue("Explicit isolated Linux fixture required",endpoint.isNotEmpty())
  val ca=String(Base64.decode(args.getString("mdm_ca"),Base64.DEFAULT),Charsets.UTF_8)
  val c=Isolated(InstrumentationRegistry.getInstrumentation().targetContext)
  try{
   VpnProfiles.list(c);val store=MdmStore(c)
   val controller=MdmController(store,{object:MdmGateway{
    var transport:MdmTransport?=null
    override fun post(b:MdmBinding,op:String,body:JSONObject,secret:String):JSONObject{
     if(op=="sync")body.put("wait",false)
     val t=MdmTransport(c,{secret},ca);transport=t
     return JSONObject(t.post(b,op,body.toString()))
    }
    override fun close(){transport?.close()}
   }},{MdmConfiguration.detach(c)})
   controller.enroll(MdmInvitation(endpoint,args.getString("mdm_token")!!,MdmRights(config=true,vpn=true)),MdmRights(config=true,vpn=true))
   assertTrue(controller.poll{_,_->}) // Deliver granted rights before sending report.
   assertTrue(controller.report{s->MdmDeviceReport.snapshot(c,s).put("name","Wire test")})
   var desired:MdmConfigRevision?=null
   assertTrue(controller.poll{_,response->desired=response.desiredConfig})
   assertNotNull("Server did not create desired config from report",desired)
   val port=object:MdmVpnPort{override fun active()=false;override fun stop(){};override fun start()="stopped"}
   val result=MdmApplyCoordinator(c,port).apply(store.read(),desired!!)
   assertTrue(result.errorCode,result.configurationApplied)
   assertEquals("org.example.selected",MdmConfiguration.snapshotLocal(c).getJSONArray("globalApps").getString(0))
   controller.report{s->MdmDeviceReport.snapshot(c,s).put("name","Wire test")}
   controller.poll{_,response->assertNull("Applied revision must be acknowledged",response.desiredConfig)}
   assertEquals(desired!!.revision,store.read().appliedRevision)
   controller.pause();controller.delete()
  }finally{c.close()}
 }
}
