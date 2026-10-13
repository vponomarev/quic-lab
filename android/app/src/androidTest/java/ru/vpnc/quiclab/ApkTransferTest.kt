package ru.vpnc.quiclab

import org.junit.Assert.*
import org.junit.Test
import java.io.ByteArrayInputStream
import java.io.ByteArrayOutputStream

class ApkTransferTest {
 @Test fun processDeathReportsRetryAndRejectsStaleUpdates(){
  val c=androidx.test.platform.app.InstrumentationRegistry.getInstrumentation().targetContext
  val prefix="apk-state-test-"+System.nanoTime()
  val isolated=object:android.content.ContextWrapper(c){override fun getSharedPreferences(name:String,mode:Int)=baseContext.getSharedPreferences(prefix+name,mode)}
  try {
   val p=isolated.getSharedPreferences("apk_download",0)
   p.edit().putBoolean("owned",true).putLong("id",7).putInt("status",android.app.DownloadManager.STATUS_RUNNING).putLong("bytes",123).commit()
   val state=ApkDownloads.snapshot(isolated)
   assertEquals(android.app.DownloadManager.STATUS_FAILED,state.status)
   assertEquals(123L,state.bytes)
   assertTrue(state.message.contains("Повторить"))
   assertFalse(OwnedApkDownloads.update(isolated,6,android.app.DownloadManager.STATUS_SUCCESSFUL,999,999,"stale"))
   assertEquals(7L,ApkDownloads.snapshot(isolated).id)
  }finally{c.deleteSharedPreferences(prefix+"apk_download")}
 }
 @Test fun copiesCompleteBodyAndReportsProgress(){
  val data=ByteArray(180000){(it%251).toByte()};val out=ByteArrayOutputStream();var progress=0L
  ApkTransfer.copyBody(ByteArrayInputStream(data),out,data.size.toLong(),200000,{true}){progress=it}
  assertArrayEquals(data,out.toByteArray());assertEquals(data.size.toLong(),progress)
 }
 @Test fun rejectsTruncatedOversizedAndCancelledTransfers(){
  fun copy(data:ByteArray,total:Long,max:Long,active:Boolean=true)=runCatching{
   ApkTransfer.copyBody(ByteArrayInputStream(data),ByteArrayOutputStream(),total,max,{active}){}
  }
  assertTrue(copy(ByteArray(10),11,100).isFailure)
  assertTrue(copy(ByteArray(10),-1,9).isFailure)
  assertTrue(copy(ByteArray(10),10,100,false).isFailure)
  assertTrue(copy(ByteArray(0),-1,100).isFailure)
  assertTrue(copy(ByteArray(10),-1,100).isSuccess)
 }
 @Test fun redirectsNeverDowngradeToCleartext(){
  assertEquals("https://example.org/new.apk",ApkTransfer.redirect("https://example.org/old.apk","/new.apk"))
  assertTrue(runCatching{ApkTransfer.redirect("https://example.org/a","http://example.org/a")}.isFailure)
 }
}
