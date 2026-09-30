package ru.vpnc.quiclab

import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.UUID
import org.json.JSONArray
import org.junit.Assert.*
import org.junit.Test

class VpnConfigurationTest {
 private class IsolatedContext(base: Context): ContextWrapper(base) {
  val prefix = "migration-test-" + UUID.randomUUID().toString()
  val names = mutableSetOf<String>()
  private val directory = File(base.cacheDir, prefix).apply { check(mkdirs()) }
  override fun getFilesDir(): File = directory
  override fun getSharedPreferences(name: String, mode: Int): SharedPreferences {
   names.add(prefix + name)
   return baseContext.getSharedPreferences(prefix + name, mode)
  }
  fun clean() {
   names.forEach { baseContext.deleteSharedPreferences(it) }
   check(directory.parentFile == baseContext.cacheDir)
   directory.deleteRecursively()
  }
 }
 private fun withContext(block: (IsolatedContext) -> Unit) {
  val c = IsolatedContext(InstrumentationRegistry.getInstrumentation().targetContext)
  try { block(c) } finally { c.clean() }
 }
 private fun seed(c: Context) {
  c.getSharedPreferences("vpn_profiles",0).edit()
   .putString("profiles", """[{"id":"default","name":"Home"},{"id":"11111111-1111-1111-1111-111111111111","name":"Internet"}]""")
   .putString("current","default").putBoolean("multiple",true)
   .putStringSet("multiple_enabled",setOf("default","11111111-1111-1111-1111-111111111111"))
   .putString("multiple_dns","default").putStringSet("global_apps",setOf("example.shared")).commit()
  c.getSharedPreferences("vpn",0).edit().putString("transport","awg")
   .putString("endpoint","home.example:51820").putString("dns","10.0.0.1")
   .putInt("mode",3).putString("routes","192.168.1.0/24").commit()
  c.getSharedPreferences("vpn_11111111-1111-1111-1111-111111111111",0).edit()
   .putString("transport","quic").putString("endpoint","net.example:443")
   .putBoolean("max_availability",true).putBoolean("global_apps",false)
   .putStringSet("apps",setOf("example.private")).commit()
  File(c.filesDir,"vpn-identity.enc").writeBytes(byteArrayOf(9,8,7))
 }
 @Test fun migrationIsIdempotentAndPreservesSettings() = withContext { c ->
  seed(c)
  val first = VpnConfiguration.migrate(c)
  assertEquals(first.toString(),VpnConfiguration.migrate(c).toString())
  val model = first.getJSONObject("model")
  assertEquals(2,model.getJSONArray("exits").length())
  assertEquals("standalone",model.getJSONArray("exits").getJSONObject(0).getString("kind"))
  assertEquals("demux",model.getJSONArray("exits").getJSONObject(1).getString("kind"))
  assertEquals("default",model.getJSONArray("profiles").getJSONObject(0).getString("exit_id"))
  val local = first.getJSONObject("local")
  assertEquals("default",local.getString("dns_exit_id"))
  assertEquals("192.168.1.0/24",local.getJSONObject("profiles").getJSONObject("default").getString("routes"))
  assertEquals("example.private",local.getJSONObject("profiles").getJSONObject("11111111-1111-1111-1111-111111111111").getJSONArray("apps").getString(0))
  assertArrayEquals(byteArrayOf(9,8,7),File(c.filesDir,"vpn-identity.enc").readBytes())
  assertFalse(first.toString().contains("PRIVATE KEY"))
 }
 @Test fun failedWriteKeepsOldConfigurationAndIdentity() = withContext { c ->
  seed(c); VpnConfiguration.migrate(c)
  val file=File(c.filesDir,"vpn-configuration.json")
  val before=file.readBytes()
  c.getSharedPreferences("vpn",0).edit().putString("endpoint","new.example:51820").commit()
  val blocker=File(c.filesDir,"vpn-configuration.json.new")
  check(blocker.mkdirs()); File(blocker,"block").writeText("cannot replace this directory")
  try { VpnConfiguration.migrate(c); fail("expected failed atomic write") } catch (_: java.io.IOException) {}
  assertArrayEquals(before,file.readBytes())
  assertArrayEquals(byteArrayOf(9,8,7),File(c.filesDir,"vpn-identity.enc").readBytes())
 }
 @Test fun emptyDefaultProfileMigratesAsDisabledDraft() = withContext { c ->
  val config=VpnConfiguration.load(c)
  assertEquals("disabled",config.getJSONObject("model").getJSONArray("profiles").getJSONObject(0).getString("mode"))
 }
 @Test fun editingLegacyProfileRefreshesSnapshot() = withContext { c ->
  seed(c); VpnConfiguration.migrate(c)
  c.getSharedPreferences("vpn",0).edit().putString("endpoint","new.example:51820").commit()
  val updated=VpnConfiguration.load(c)
  assertEquals("new.example:51820",updated.getJSONObject("model").getJSONArray("profiles").getJSONObject(0).getString("endpoint"))
  assertEquals(2,updated.getJSONObject("model").getJSONArray("profiles").length())
 }
 @Test fun ordinaryTransportsStayStandalone() = withContext { c ->
  for (transport in listOf("quic","https","awg")) {
   c.getSharedPreferences("vpn",0).edit().putString("transport",transport).putString("endpoint","vpn.example:443").commit()
   assertEquals("standalone",VpnConfiguration.load(c).getJSONObject("model").getJSONArray("exits").getJSONObject(0).getString("kind"))
  }
 }
 private fun importedProfile(): org.json.JSONObject {
  val fake=android.util.Base64.encodeToString(ByteArray(32){1},android.util.Base64.NO_WRAP)
  val raw="[Interface]\nPrivateKey = $fake\nAddress = 10.20.0.2\nDNS = 1.1.1.1\n[Peer]\nPublicKey = $fake\nAllowedIPs = 0.0.0.0/0\nEndpoint = vpn.example:52000\n"
  return org.json.JSONObject().put("version",2).put("kind","vpn").put("hostname","vpn.example")
   .put("transports",JSONArray().put("awg")).put("awg_config",raw)
 }
 @Test fun importPublishesConfigurationWithoutSecrets() = withContext { c ->
  seed(c)
  ProfileImport.save(c,importedProfile())
  val text=File(c.filesDir,"vpn-configuration.json").readText()
  assertEquals(3,org.json.JSONObject(text).getJSONObject("model").getJSONArray("profiles").length())
  assertFalse(text.contains("PrivateKey"))
  assertTrue(VpnProfiles.identityFile(c).exists())
 }
 @Test fun importRollsBackWhenConfigurationCannotBeWritten() = withContext { c ->
  seed(c); VpnConfiguration.migrate(c)
  val file=File(c.filesDir,"vpn-configuration.json")
  val before=file.readBytes()
  val blocker=File(c.filesDir,"vpn-configuration.json.new")
  check(blocker.mkdirs()); File(blocker,"block").writeText("block write")
  try { ProfileImport.save(c,importedProfile()); fail("expected failed import") } catch (_: java.io.IOException) {}
  assertEquals(2,VpnProfiles.list(c).size)
  assertEquals("default",VpnProfiles.current(c).id)
  assertArrayEquals(before,file.readBytes())
  assertEquals(listOf("vpn-identity.enc"),c.filesDir.listFiles()!!.filter { it.name.endsWith(".enc") }.map { it.name })
 }
}
