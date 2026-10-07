package ru.vpnc.quiclab
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File

class MdmStoreTest {
 @Test fun encryptedStateSurvivesRestartAndSecretsAreNotDisplayState(){
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val f=File(c.noBackupFilesDir,"mdm-test-"+System.nanoTime())
  val alias="quic-lab-mdm-test-"+System.nanoTime()
  try{
   val store=MdmStore(f,alias)
   store.edit{it.put("secret","sensitive-mdm-credential").put("generation",7)}
   assertFalse(f.readText().contains("sensitive-mdm-credential"))
   assertEquals(7,MdmStore(f,alias).read().localGeneration)
   assertEquals("sensitive-mdm-credential",MdmStore(f,alias).secret())
   store.erase()
   assertNull(MdmStore(f,alias).read().binding)
   assertEquals("",MdmStore(f,alias).secret())
  }finally{f.delete();java.security.KeyStore.getInstance("AndroidKeyStore").apply{load(null);deleteEntry(alias)}}
 }
 @Test fun corruptStateFailsClosed(){
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val f=File(c.noBackupFilesDir,"mdm-corrupt-"+System.nanoTime())
  try{f.writeText("broken");assertTrue(runCatching{MdmStore(f,"unused-mdm-key").read()}.isFailure)}finally{f.delete()}
 }
}
