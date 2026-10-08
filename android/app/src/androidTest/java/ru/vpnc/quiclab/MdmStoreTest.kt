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
 @Test fun repeatedPreferenceReadsStayBelowFrameBlockingBudget(){
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val f=File(c.noBackupFilesDir,"mdm-read-budget-"+System.nanoTime());val alias="mdm-read-budget-"+System.nanoTime()
  try{
   MdmStore(f,alias).edit{it.put("generation",7).put("payload","x".repeat(8192))}
   val started=android.os.SystemClock.elapsedRealtime()
   repeat(100){assertEquals(7L,MdmStore(f,alias).read().localGeneration)}
   val elapsed=android.os.SystemClock.elapsedRealtime()-started
   InstrumentationRegistry.getInstrumentation().sendStatus(0,android.os.Bundle().apply{putLong("repeated_read_ms",elapsed)})
   assertTrue("100 preference reads blocked for $elapsed ms",elapsed<1000)
  }finally{f.delete();java.security.KeyStore.getInstance("AndroidKeyStore").apply{load(null);deleteEntry(alias)}}
 }
 @Test fun returnedDocumentIsDetachedAndExternalCorruptionFailsClosed(){
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val f=File(c.noBackupFilesDir,"mdm-cache-integrity-"+System.nanoTime());val alias="mdm-cache-integrity-"+System.nanoTime()
  try{
   val store=MdmStore(f,alias);store.edit{it.put("generation",7)}
   store.document().put("generation",999)
   assertEquals(7L,MdmStore(f,alias).read().localGeneration)
   val original=f.readBytes();val broken=original.copyOf();broken[broken.lastIndex]=(broken.last().toInt() xor 1).toByte();f.writeBytes(broken)
   assertTrue("Changed ciphertext must never use cached plaintext",runCatching{MdmStore(f,alias).read()}.isFailure)
   f.writeBytes(original);assertEquals(7L,MdmStore(f,alias).read().localGeneration)
   store.edit{it.put("generation",8)};assertEquals(8L,MdmStore(f,alias).read().localGeneration)
  }finally{f.delete();java.security.KeyStore.getInstance("AndroidKeyStore").apply{load(null);deleteEntry(alias)}}
 }

}
