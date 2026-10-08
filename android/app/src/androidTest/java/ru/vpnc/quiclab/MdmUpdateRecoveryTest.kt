package ru.vpnc.quiclab

import android.content.Intent
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.io.File

/** Explicit, two-stage acceptance fixture on the dedicated unbound test phone only.
 * Between prepare and cleanup, reinstall APK and inspect real services outside instrumentation.
 * No telemetry can leave the phone: fixture endpoint is loopback on a closed port.
 */
class MdmUpdateRecoveryTest {
 private val c get()=InstrumentationRegistry.getInstrumentation().targetContext
 private fun enabled(){assumeTrue(InstrumentationRegistry.getArguments().getString("update_fixture")=="true")}
 @Test fun prepare(){
  enabled()
  val store=MdmStore(c);val backup=File(c.noBackupFilesDir,"radio-update-test-backup.json")
  check(!backup.exists() && store.read().binding==null && !store.read().pendingEnrollment && !LabVpnService.active)
  backup.writeText(store.document().toString())
  store.edit{it.put("binding",MdmStore.bindingJson(MdmBinding("update-test","https://127.0.0.1:9",1,MdmRights(),true)))
   .put("active",true).put("rights",MdmRights(telemetry=true,geo=true).json()).put("secret","a".repeat(64))}
  MdmTelemetry.setManual(c,true)
  assertTrue(MdmStore(c).manualTelemetry())
 }
 @Test fun cleanup(){
  enabled()
  val backup=File(c.noBackupFilesDir,"radio-update-test-backup.json")
  check(backup.exists() && MdmStore(c).read().binding?.id=="update-test")
  MdmTelemetry.stop(c,true);MdmService.stop(c)
  val original=JSONObject(backup.readText())
  MdmStore(c).edit{j->j.keys().asSequence().toList().forEach{j.remove(it)};original.keys().forEach{k->j.put(k,original.get(k))}}
  backup.delete()
  assertFalse(MdmTelemetry.wanted(c))
 }
}
