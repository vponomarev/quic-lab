package ru.vpnc.quiclab

import android.content.Context
import android.content.ContextWrapper
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File

/** Cross-device acceptance: producer and consumer exchange only a synthetic fixture. */
class ConfigurationTransferDeviceTest {
 private val real get()=InstrumentationRegistry.getInstrumentation().targetContext
 private val password="synthetic-transfer-acceptance"
 private val uri="vless://11111111-1111-4111-8111-111111111111@example.org:443?security=tls&type=tcp&sni=example.org"
 private fun isolated(block:(Context)->Unit){
  val base=real;val prefix="cross-transfer-${System.nanoTime()}";val names=mutableSetOf<String>()
  val dir=File(base.noBackupFilesDir,prefix).apply{mkdirs()}
  val c=object:ContextWrapper(base){
   override fun getNoBackupFilesDir()=dir
   override fun getFilesDir()=dir
   override fun getApplicationContext():Context=this
   override fun getSharedPreferences(name:String,mode:Int)=base.getSharedPreferences("$prefix-$name".also{names.add(it)},mode)
  }
  try{VpnProfiles.list(c);block(c)}finally{dir.deleteRecursively();names.forEach{base.deleteSharedPreferences(it)}}
 }
 @Test fun exportFixture(){
  org.junit.Assume.assumeTrue(InstrumentationRegistry.getArguments().getString("transferDevice")=="export")
  isolated{c->
  MdmConfigurationStore(c).initialize(MdmConfiguration.snapshotLocal(c),MdmConfiguration.snapshotRuntime(c))
  assertTrue(MdmConfiguration.writeIdentity(c,"default",JSONObject().put("vless_config",mobile.Mobile.importVLESSConfig(uri)).put("update_token","private-source-token")))
  val bytes=ConfigurationTransfer.export(c,listOf("profiles"),emptyList(),true,password)
  assertFalse(bytes.toString(Charsets.ISO_8859_1).contains("11111111-1111"))
  File(real.getExternalFilesDir(null),"cross-device-fixture.age").writeBytes(bytes)
 }
 }
 @Test fun importFixtureAndRollback(){
  org.junit.Assume.assumeTrue(InstrumentationRegistry.getArguments().getString("transferDevice")=="import")
  isolated{c->
  val bytes=File(real.getExternalFilesDir(null),"cross-device-fixture.age").readBytes()
  assertTrue(runCatching{ConfigurationTransfer.decode(bytes,"incorrect-password")}.isFailure)
  val decoded=ConfigurationTransfer.decode(bytes,password)
  assertFalse(decoded.contains("private-source-token"));assertFalse(decoded.contains("update_token"))
  val before=ConfigurationTransfer.snapshot(c,true).toString()
  val p=ConfigurationTransfer.prepare(c,decoded,"add",mapOf("default" to "22222222-2222-4222-8222-222222222222"))
  assertEquals(before,ConfigurationTransfer.snapshot(c,true).toString())
  assertTrue(p.missing.isEmpty());assertTrue(p.missingApps.isEmpty())
  val port=object:MdmVpnPort{override fun active()=false;override fun stop(){fail("idle fixture must not stop VPN")};override fun start():String{fail("idle fixture must not start VPN");return "unexpected"}}
  LocalConfigurationApply.apply(c,p,port)
  val profiles=ConfigurationTransfer.snapshot(c,true).getJSONArray("profiles")
  val imported=(0 until profiles.length()).map{profiles.getJSONObject(it)}.single{it.getString("id")=="22222222-2222-4222-8222-222222222222"}
  assertTrue(imported.getJSONObject("identity").toString().contains("11111111-1111-4111-8111-111111111111"))
  LocalConfigurationApply.apply(c,ConfigurationTransfer.rollback(c),port)
  assertEquals(before,ConfigurationTransfer.snapshot(c,true).toString())
 }
}
}
