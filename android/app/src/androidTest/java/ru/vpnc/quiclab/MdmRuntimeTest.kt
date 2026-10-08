package ru.vpnc.quiclab
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit

class MdmRuntimeTest {
 @Test fun latePollCannotReachConfigurationAfterPause(){
  val context=InstrumentationRegistry.getInstrumentation().targetContext
  val file=File(context.noBackupFilesDir,"mdm-runtime-"+System.nanoTime())
  val alias="mdm-runtime-"+System.nanoTime()
  val entered=CountDownLatch(1);val reply=CountDownLatch(1)
  val store=MdmStore(file,alias)
  try{
   store.edit{j->j.put("binding",MdmStore.bindingJson(MdmBinding("device","https://mdm.example",1,MdmRights(config=true),true)))
    .put("active",true).put("secret","test").put("rights",MdmRights(config=true).json())}
   val controller=MdmController(store,{object:MdmGateway{
    override fun close(){}
    override fun post(b:MdmBinding,op:String,body:JSONObject,secret:String):JSONObject{
     if(op=="sync"){entered.countDown();check(reply.await(3,TimeUnit.SECONDS))}
     return JSONObject("""{"version":1,"epoch":1,"commands":[],"serverTime":"2026-10-08T00:00:00Z"}""")
    }
   }},{})
   var delivered=false
   val worker=Thread{runCatching{controller.poll{_,_->delivered=true}}}
   worker.start();assertTrue(entered.await(2,TimeUnit.SECONDS))
   controller.pause();reply.countDown();worker.join(3000)
   assertFalse(worker.isAlive);assertFalse(delivered)
   assertFalse(controller.poll{_,_->fail("paused polling")})
  }finally{reply.countDown();file.delete();java.security.KeyStore.getInstance("AndroidKeyStore").apply{load(null);deleteEntry(alias)}}
 }
}
