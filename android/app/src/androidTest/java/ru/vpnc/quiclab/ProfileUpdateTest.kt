package ru.vpnc.quiclab

import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class ProfileUpdateTest {
 private val device="11111111-1111-4111-8111-111111111111"
 private fun envelope(revision:Long=2):JSONObject = JSONObject()
  .put("schema_version",1).put("config_revision",revision).put("device_id",device).put("exit_id",device)
  .put("server_config",JSONObject().put("version",2).put("kind","vpn").put("hostname","vpn.example")
   .put("transports",org.json.JSONArray().put("quic")).put("quic","vpn.example:443")
   .put("certificate","placeholder").put("key","placeholder").put("dns","1.1.1.1"))
  .put("capabilities",JSONObject().put("control_version",1).put("data_version",1).put("min_android_version_code",0))
 private fun denied(block:()->Unit) { try {block();fail("Expected rejection")} catch(_:IllegalArgumentException) {} }
 @Test fun atomicApplyAndLocalPreferences() {
  val current=envelope(4)
  denied {ProfileUpdate.validateCandidate(current,envelope(3),device)}
  val incoming=envelope(5)
  incoming.getJSONObject("server_config").put("routes","0.0.0.0/0").put("mode",0)
  denied {ProfileUpdate.validateCandidate(current,incoming,device)}
  val local=JSONObject().put("routes","192.168.1.0/24").put("mode",3).put("bond_cell_budget",17)
  val before=current.toString();val localBefore=local.toString()
  denied {ProfileUpdate.validateCandidate(current,envelope(5).put("device_id","other"),device)}
  assertEquals(before,current.toString());assertEquals(localBefore,local.toString())
  ProfileUpdate.validateCandidate(current,envelope(5),device)
  assertEquals(localBefore,local.toString())
 }
 @Test fun diffDoesNotExposeCredentials() {
  val current=envelope(1);val incoming=envelope(2)
  incoming.getJSONObject("server_config").put("quic","new.example:443").put("key","SECRET-NEW-KEY")
  val diff=ProfileUpdate.diff(current,incoming).toString()
  assertTrue(diff.contains("quic"));assertFalse(diff.contains("SECRET-NEW-KEY"))
 }
 @Test fun apkValidation() {
  val hash="a".repeat(64);val signer="b".repeat(64);val pkg="ru.vpnc.quiclab"
  ProfileUpdate.validateApkMetadata(hash,hash,pkg,pkg,setOf(signer),setOf(signer))
  denied {ProfileUpdate.validateApkMetadata(hash,"c".repeat(64),pkg,pkg,setOf(signer),setOf(signer))}
  denied {ProfileUpdate.validateApkMetadata(hash,hash,"other.package",pkg,setOf(signer),setOf(signer))}
  denied {ProfileUpdate.validateApkMetadata(hash,hash,pkg,pkg,setOf("c".repeat(64)),setOf(signer))}
  denied {ProfileUpdate.validateApkMetadata(hash,hash,pkg,pkg,emptySet(),emptySet())}
 }
 private class IsolatedContext(base:android.content.Context):android.content.ContextWrapper(base) {
  val prefix="update-test-"+java.util.UUID.randomUUID()
  val names=mutableSetOf<String>()
  private val directory=java.io.File(base.cacheDir,prefix).apply {check(mkdirs())}
  override fun getFilesDir()=directory
  override fun getSharedPreferences(name:String,mode:Int):android.content.SharedPreferences {
   names.add(prefix+name);return baseContext.getSharedPreferences(prefix+name,mode)
  }
  fun clean() {names.forEach {baseContext.deleteSharedPreferences(it)};directory.deleteRecursively()}
 }
 private fun withContext(block:(IsolatedContext)->Unit) {
  val c=IsolatedContext(androidx.test.platform.app.InstrumentationRegistry.getInstrumentation().targetContext)
  try {block(c)} finally {c.clean()}
 }
 private fun awgEnvelope(revision:Long):JSONObject {
  val key=android.util.Base64.encodeToString(ByteArray(32) {(it+1).toByte()},android.util.Base64.NO_WRAP)
  val raw="[Interface]\nPrivateKey = $key\nAddress = 10.20.0.2\nDNS = 1.1.1.1\n[Peer]\nPublicKey = $key\nAllowedIPs = 0.0.0.0/0\nEndpoint = vpn.example:52000\n"
  return envelope(revision).put("server_config",JSONObject().put("version",2).put("kind","vpn").put("hostname","vpn.example")
   .put("transports",org.json.JSONArray().put("awg")).put("awg_config",raw).put("dns","1.1.1.1")
   .put("config_url","https://control.example/api/v1/devices/$device/config"))
 }
 private fun seed(c:android.content.Context,current:JSONObject) {
  VpnProfiles.list(c)
  c.getSharedPreferences("vpn",0).edit().putBoolean("managed_profile",true).putString("transport","awg")
   .putString("routes","192.168.1.0/24").putInt("mode",3).putLong("bond_copy_kib",17).commit()
  val server=current.getJSONObject("server_config")
  VpnIdentity.writeBundle(c,"default",JSONObject().put("device_id",device).put("update_token","a".repeat(64))
   .put("config_url",server.getString("config_url")).put("awg_config",server.getString("awg_config"))
   .put("server_config",server).put("update_envelope",current))
 }
 @Test fun atomicWriteFailureAndValidationPreserveCommittedBundle()=withContext {c->
  seed(c,awgEnvelope(1));val file=VpnProfiles.identityFile(c,"default");val before=file.readBytes()
  val local=c.getSharedPreferences("vpn",0).all.toMap()
  denied {ProfileUpdate.apply(c,"default",awgEnvelope(0))}
  assertArrayEquals(before,file.readBytes())
  val blocker=java.io.File(file.path+".new").apply {check(mkdirs())}
  java.io.File(blocker,"block").writeText("block")
  try {ProfileUpdate.apply(c,"default",awgEnvelope(2));fail("write should fail")} catch(_:java.io.IOException) {}
  assertArrayEquals(before,file.readBytes());assertEquals(local,c.getSharedPreferences("vpn",0).all)
  blocker.deleteRecursively()
  ProfileUpdate.apply(c,"default",awgEnvelope(2))
  assertEquals(2,VpnIdentity.load(c,"default").getJSONObject("update_envelope").getInt("config_revision"))
  assertEquals(local,c.getSharedPreferences("vpn",0).all)
  assertEquals("192.168.1.0/24",VpnProfiles.preferences(c,"default").getString("routes",""))
 }
 @Test fun selectedTransportUsesUpdatedEndpoint()=withContext {c->
  val server=envelope(1).getJSONObject("server_config").put("transports",org.json.JSONArray().put("quic").put("https"))
   .put("quic","new-quic.example:443").put("https","new-https.example:8443")
  c.getSharedPreferences("vpn",0).edit().putBoolean("managed_profile",true).putString("transport","https")
   .putString("endpoint","old.example:443").putString("routes","10.0.0.0/8").commit()
  VpnIdentity.writeBundle(c,"default",JSONObject().put("server_config",server))
  assertEquals("https",VpnProfiles.preferences(c,"default").getString("transport",""))
  assertEquals("new-https.example:8443",VpnProfiles.preferences(c,"default").getString("endpoint",""))
  assertEquals("10.0.0.0/8",VpnProfiles.preferences(c,"default").getString("routes",""))
  c.getSharedPreferences("vpn",0).edit().putString("transport","quic").commit()
  assertEquals("new-quic.example:443",VpnProfiles.preferences(c,"default").getString("endpoint",""))
 }
 @Test fun applyInternetOnlyRestartsInternetAndPreservesBudget()=withContext {c->
  org.junit.Assume.assumeTrue(!LabVpnService.active)
  seed(c,awgEnvelope(1))
  class Session(val revision:Long):AutoCloseable {var closes=0;override fun close(){closes++}}
  val controller=VpnExitController(setOf("home","default")) {id,_->
   Session(if(id=="default") VpnIdentity.load(c,id).getJSONObject("update_envelope").getLong("config_revision") else 1)
  }
  val homeGeneration=controller.start("home");val internetGeneration=controller.start("default")
  val home=controller.session("home")!!;val internet=controller.session("default")!!
  val run=VpnBudgetRun();val budget=run.start(1234);val before=JSONObject(budget.snapshot()).getString("epoch")
  val tunToken=Any();val previousRuntime=LabVpnService.updateRuntime
  val runtime=VpnProfileUpdateRuntime("home",budget,tunToken) {id->controller.update(id,false) {}}
  try {
   LabVpnService.updateRuntime=runtime
   ProfileUpdate.apply(c,"default",awgEnvelope(2))
   assertTrue(controller.current("home",homeGeneration));assertFalse(controller.current("default",internetGeneration))
   assertEquals(0,home.closes);assertEquals(1,internet.closes)
   assertEquals(2L,controller.session("default")!!.revision)
   assertSame(runtime,LabVpnService.updateRuntime);assertSame(budget,LabVpnService.updateRuntime!!.budget)
   assertSame(tunToken,LabVpnService.updateRuntime!!.tunToken)
   assertEquals(before,JSONObject(LabVpnService.updateRuntime!!.budget.snapshot()).getString("epoch"))
  } finally {LabVpnService.updateRuntime=previousRuntime;controller.stopAll()}
 }
 @Test fun pausedExitReloadsBeforeResume() {
  class Session(val revision:Int):AutoCloseable {override fun close(){}}
  var revision=1
  val controller=VpnExitController(setOf("internet")) {_,_->Session(revision)}
  controller.start("internet");controller.stop("internet")
  assertNull(controller.update("internet",true) {revision=2})
  assertNull(controller.session("internet"))
  controller.start("internet")
  assertEquals(2,controller.session("internet")!!.revision);controller.stopAll()
 }
 @Test fun activeDnsChangeRequiresStopRegardlessOfSelectedUiProfile()=withContext {c->
  org.junit.Assume.assumeTrue(!LabVpnService.active)
  seed(c,awgEnvelope(1));val before=VpnProfiles.identityFile(c,"default").readBytes()
  val previousRuntime=LabVpnService.updateRuntime
  try {
   LabVpnService.updateRuntime=VpnProfileUpdateRuntime("default",mobile.Mobile.newTrafficBudget("dns-update-test",0),Any()) {}
   VpnProfiles.create(c,"Other selected UI profile")
   assertNotEquals("default",VpnProfiles.current(c).id)
   assertTrue(LabVpnService.isActiveDnsExit(c,"default"))
   val incoming=awgEnvelope(2);incoming.getJSONObject("server_config").put("dns","9.9.9.9")
   denied {ProfileUpdate.apply(c,"default",incoming)}
   assertArrayEquals(before,VpnProfiles.identityFile(c,"default").readBytes())
  } finally {LabVpnService.updateRuntime=previousRuntime}
 }
 @Test fun realApkHashPackageSignerCheckedBeforeInstallerIntent() {
  val inst=androidx.test.platform.app.InstrumentationRegistry.getInstrumentation();val c=inst.targetContext
  val source=java.io.File(c.applicationInfo.sourceDir)
  val hash=java.security.MessageDigest.getInstance("SHA-256").digest(source.readBytes()).joinToString("") {"%02x".format(it.toInt() and 255)}
  ProfileUpdate.validateApk(c,source,hash)
  denied {ProfileUpdate.validateApk(c,source,"0".repeat(64))}
  val other=java.io.File(inst.context.applicationInfo.sourceDir)
  val otherHash=java.security.MessageDigest.getInstance("SHA-256").digest(other.readBytes()).joinToString("") {"%02x".format(it.toInt() and 255)}
  denied {ProfileUpdate.validateApk(c,other,otherHash)}
  val target=java.io.File(java.io.File(c.cacheDir,"updates").apply {mkdirs()},"test-verified.apk")
  try {
   source.copyTo(target,true)
   assertEquals(android.content.Intent.ACTION_VIEW,ProfileUpdate.installerIntent(c,target,hash).action)
  } finally {target.delete()}
 } @Test fun managedManualIdentityImportCannotDiscardEnrollment()=withContext {c->
  org.junit.Assume.assumeTrue(!LabVpnService.active)
  seed(c,awgEnvelope(1));val before=VpnProfiles.identityFile(c,"default").readBytes()
  denied {VpnIdentity.import(c,byteArrayOf(1,2,3),charArrayOf())}
  denied {VpnIdentity.importAWG(c,awgEnvelope(2).getJSONObject("server_config").getString("awg_config"))}
  denied {VpnIdentity.importBundle(c,JSONObject())}
  assertArrayEquals(before,VpnProfiles.identityFile(c,"default").readBytes())
  assertEquals("a".repeat(64),VpnIdentity.load(c,"default").getString("update_token"))
 }
 @Test fun managedBackupOnlyIdentityRecoversServerConfiguration()=withContext {c->
  seed(c,awgEnvelope(1))
  c.getSharedPreferences("vpn",0).edit().putString("endpoint","stale-local.example:443").commit()
  val file=VpnProfiles.identityFile(c,"default")
  check(file.renameTo(java.io.File(file.path+".bak")))
  assertFalse(file.exists())
  assertEquals("vpn.example:52000",VpnProfiles.preferences(c,"default").getString("endpoint",""))
  assertTrue(file.exists())
  assertEquals("a".repeat(64),VpnIdentity.load(c,"default").getString("update_token"))
 }
 @Test fun readerWaitsForConfigurationTransactionMonitor()=withContext {c->
  seed(c,awgEnvelope(1))
  val attempting=java.util.concurrent.CountDownLatch(1)
  val completed=java.util.concurrent.CountDownLatch(1)
  var failure:Throwable?=null
  val reader=Thread({try {attempting.countDown();VpnIdentity.load(c,"default")}catch(e:Throwable){failure=e}finally{completed.countDown()}},"identity-reader-test")
  synchronized(MdmConfiguration.lock) {
   reader.start();assertTrue(attempting.await(5,java.util.concurrent.TimeUnit.SECONDS))
   assertFalse("Read must wait for pending AtomicFile write transaction",completed.await(1,java.util.concurrent.TimeUnit.SECONDS))
  }
  assertTrue(completed.await(5,java.util.concurrent.TimeUnit.SECONDS));failure?.let {throw it}
 }
}
