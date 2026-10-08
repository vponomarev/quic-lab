package ru.vpnc.quiclab
import androidx.test.platform.app.InstrumentationRegistry
import android.util.Base64
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import org.json.JSONObject
import org.json.JSONArray
import java.time.Instant
import java.util.UUID

class MdmTelemetryWireTest{
 @Test fun actualRadioUploadThenPauseRejectsDelivery(){
  val args=InstrumentationRegistry.getArguments()
  val endpoint=args.getString("radio_endpoint").orEmpty();assumeTrue("Requires explicit TLS fixture",endpoint.isNotBlank())
  val ca=String(Base64.decode(args.getString("mdm_ca"),Base64.DEFAULT))
  val token=args.getString("radio_token")!!
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val secret=UUID.randomUUID().toString().replace("-","")+UUID.randomUUID().toString().replace("-","")
  var b=MdmBinding("",endpoint,0,MdmRights(),false)
  fun post(op:String,body:JSONObject)=MdmTransport(c,{secret},ca).use{JSONObject(it.post(b,op,body.toString()))}
  val enrolled=post("enroll",JSONObject().put("version",1).put("token",token).put("registrationId",UUID.randomUUID().toString()).put("secret",secret))
  b=b.copy(id=enrolled.getString("id"))
  val active=post("activate",MdmSyncRequest(b.id,0,wait=false).json())
  b=b.copy(epoch=active.getLong("epoch"),active=true)
  post("sync",MdmSyncRequest(b.id,b.epoch,wait=false,grantedRights=MdmRights(telemetry=true,geo=true)).json())
  val sample=MdmRadioSampler(c).sample()
  assertTrue(sample.has("wifiStatus"));assertTrue(sample.has("cellStatus"))
  val batch=JSONObject().put("version",1).put("bindingId",b.id).put("epoch",b.epoch).put("records",JSONArray().put(sample)).put("dropped",0)
  assertTrue(post("telemetry",batch).getBoolean("accepted"))
  assertTrue(post("telemetry",batch).getBoolean("accepted"))
  if(args.getString("radio_network_loss")=="true"){
   val inst=InstrumentationRegistry.getInstrumentation()
   val cm=c.getSystemService(android.net.ConnectivityManager::class.java)
   val queueDir=java.io.File(c.noBackupFilesDir,"mdm-wire-queue-"+System.nanoTime())
   val queue=MdmTelemetryStore(queueDir)
   try{
    inst.uiAutomation.executeShellCommand("svc wifi disable").close()
    val deadline=System.nanoTime()+java.util.concurrent.TimeUnit.SECONDS.toNanos(8)
    while(System.nanoTime()<deadline && cm.allNetworks.any{cm.getNetworkCapabilities(it)?.hasTransport(android.net.NetworkCapabilities.TRANSPORT_WIFI)==true})Thread.sleep(100)
    val next=JSONObject(sample.toString()).put("id",UUID.randomUUID().toString()).put("measuredAt",Instant.now().toString())
    queue.append(next,System.currentTimeMillis())
    MdmTransport(c,{secret},ca,wifiOnly={true}).use{
     assertTrue("Wi-Fi-only leaked onto LTE",runCatching{it.post(b,"telemetry",batch.toString())}.isFailure)
    }
    assertEquals(1,queue.pending(System.currentTimeMillis()).length())
    inst.uiAutomation.executeShellCommand("svc wifi enable").close()
    val up=System.nanoTime()+java.util.concurrent.TimeUnit.SECONDS.toNanos(30)
    while(System.nanoTime()<up && cm.allNetworks.none{cm.getNetworkCapabilities(it)?.let{n->n.hasTransport(android.net.NetworkCapabilities.TRANSPORT_WIFI)&&n.hasCapability(android.net.NetworkCapabilities.NET_CAPABILITY_VALIDATED)}==true})Thread.sleep(250)
    batch.put("records",queue.pending(System.currentTimeMillis()))
    MdmTransport(c,{secret},ca,wifiOnly={true}).use{
     assertTrue(JSONObject(it.post(b,"telemetry",batch.toString())).getBoolean("accepted"))
    }
    queue.acknowledge(setOf(next.getString("id")),System.currentTimeMillis())
    assertEquals(0,queue.pending(System.currentTimeMillis()).length())
   }finally{inst.uiAutomation.executeShellCommand("svc wifi enable").close();queueDir.deleteRecursively()}
  }
  post("pause",MdmSyncRequest(b.id,b.epoch,wait=false).json())
  assertTrue(runCatching{post("telemetry",batch)}.isFailure)
 }
}
