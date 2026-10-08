package ru.vpnc.quiclab
import org.junit.Test
import org.junit.Assert.*
import java.time.Instant
class MdmVpnControlTest {
 @Test fun staleCommandsCannotExecute(){
  val b=MdmBinding("device","https://mdm.example",2,MdmRights(vpn=true),true)
  val state=MdmState(b,true,3,MdmRights(vpn=true))
  val now=Instant.parse("2026-10-08T12:00:00Z")
  val valid=MdmCommand("one",b.id,2,now.minusSeconds(1),now.plusSeconds(299),"vpn_start")
  assertEquals("",MdmVpnControl.validate(state,valid,now))
  assertEquals("expired",MdmVpnControl.validate(state,valid,now.plusSeconds(300)))
  assertEquals("stale_epoch",MdmVpnControl.validate(state,valid.copy(epoch=1),now))
  assertEquals("permission_denied",MdmVpnControl.validate(state.copy(rights=MdmRights()),valid,now))
  assertEquals("permission_denied",MdmVpnControl.validate(state.copy(active=false),valid,now))
 }
 private fun withStore(run:(android.content.Context,MdmStore)->Unit){
  val base=androidx.test.platform.app.InstrumentationRegistry.getInstrumentation().targetContext
  val dir=java.io.File(base.cacheDir,"command-test-"+java.util.UUID.randomUUID()).apply{mkdirs()}
  val c=object:android.content.ContextWrapper(base){override fun getNoBackupFilesDir()=dir}
  try{val store=MdmStore(c);store.edit{it.put("binding",MdmStore.bindingJson(MdmBinding("device","https://mdm.example",2,MdmRights(vpn=true),true))).put("active",true).put("rights",MdmRights(vpn=true).json())};run(c,store)}finally{dir.deleteRecursively()}
 }
 @Test fun duplicateAfterReopenDoesNotRepeatAction()=withStore{c,store->
  val now=Instant.now();val command=MdmCommand("durable", "device",2,now,now.plusSeconds(299),"vpn_start");var calls=0
  assertEquals("running",MdmVpnControl.execute(c,store.read(),command,now,action={calls++;"running"}))
  assertEquals("running",MdmVpnControl.execute(c,MdmStore(c).read(),command,now,action={calls++;"running"}))
  assertEquals(1,calls)
 }
 @Test fun timeSpentAfterDeliveryExpiresCommand()=withStore{c,store->
  val now=Instant.now();val command=MdmCommand("expired", "device",2,now.minusSeconds(298),now.plusSeconds(1),"vpn_start");var calls=0
  assertEquals("expired",MdmVpnControl.execute(c,store.read(),command,now,android.os.SystemClock.elapsedRealtime()-2000,action={calls++;"running"}))
  assertEquals(0,calls)
 }

 @Test fun repeatedConfigFailureDoesNotFloodAudit()=withStore{c,store->
  repeat(3){MdmVpnControl.event(c,"config_result","generation_conflict",12)}
  assertEquals(1,store.document().getJSONArray("events").length())
  MdmVpnControl.event(c,"config_result","applied",12)
  assertEquals(2,store.document().getJSONArray("events").length())
 }

}
