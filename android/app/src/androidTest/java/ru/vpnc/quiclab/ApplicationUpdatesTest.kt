package ru.vpnc.quiclab
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test
import org.json.JSONObject
class ApplicationUpdatesTest {
 @Test fun updateCheckUsesInternetInsteadOfImsOnRealPhone(){
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  org.junit.Assume.assumeTrue(InstrumentationRegistry.getArguments().getString("liveUpdates")=="true")
  val done=java.util.concurrent.CountDownLatch(1)
  ApplicationUpdates.check(c,true){done.countDown()}
  assertTrue(done.await(60,java.util.concurrent.TimeUnit.SECONDS))
  val status=ApplicationUpdates.status(c)
  assertFalse(status,status.contains("DNS"))
  assertTrue(status,ApplicationUpdates.offers(c).any{it.server=="quic-demo.vpnc.ru"})
 }

 @Test fun validatedImsIsNotAnInternetNetwork(){
  assertFalse(ApplicationUpdates.usableNetwork(false,true,false,false)) // operator IMS
  assertTrue(ApplicationUpdates.usableNetwork(true,true,true,false)) // public LTE / Wi-Fi
  assertFalse(ApplicationUpdates.usableNetwork(true,true,true,true)) // VPN
  assertFalse(ApplicationUpdates.usableNetwork(true,false,true,false)) // no validated internet
  assertFalse(ApplicationUpdates.usableNetwork(true,true,false,false)) // restricted network
 }
 @Test fun optionalVersionsAndHttpsOnly(){
  assertNull(ApplicationUpdates.parse("p","s",JSONObject()))
  val caps=JSONObject().put("android_version_code",36).put("android_version_name","0.8.1-pre.3").put("apk_url","https://server.test/lab/download/quic-lab.apk")
  assertEquals(36,ApplicationUpdates.parse("p","s",caps)!!.code)
  caps.put("apk_url","http://server.test/app.apk")
  assertTrue(runCatching{ApplicationUpdates.parse("p","s",caps)}.isFailure)
 }
 @Test fun journalUpgradePreservesUnknownHistoricalVersion(){
  val c=InstrumentationRegistry.getInstrumentation().targetContext
  val name="version-migration-"+System.nanoTime()+".db"
  c.openOrCreateDatabase(name,0,null).use{db->
   db.execSQL("CREATE TABLE records(seq INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT UNIQUE NOT NULL,profile TEXT NOT NULL,target TEXT NOT NULL,time INTEGER NOT NULL,kind TEXT NOT NULL,text TEXT NOT NULL,size INTEGER NOT NULL,acked INTEGER NOT NULL DEFAULT 0)")
   db.execSQL("INSERT INTO records(id,profile,target,time,kind,text,size) VALUES('old','p','s',1,'event','old',3)")
   db.version=1
  }
  val db=DiagnosticsJournal(c,name)
  try {
   assertEquals(0,db.pending("p","s").single().appVersionCode)
   assertFalse(db.pending("p","s").single().json().has("app_version"))
   db.append("p","s","event","new")
   val fresh=db.pending("p","s").last()
   assertEquals(BuildConfig.VERSION_CODE,fresh.appVersionCode)
   assertEquals(BuildConfig.VERSION_NAME,fresh.json().getString("app_version"))
  }finally{db.close();c.deleteDatabase(name)}
 }
}
