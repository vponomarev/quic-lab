package ru.vpnc.quiclab
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test
import java.io.File

class ApkDownloadTest {
 private val c get()=InstrumentationRegistry.getInstrumentation().targetContext
 @Test fun rejectsWrongPackageSignerAndVersion(){
  fun valid(pkg:String="ru.vpnc.quiclab",version:Long=100,signers:Set<String> = setOf("a")) =
   ApkDownloads.validateIdentity(pkg,version,signers,"ru.vpnc.quiclab",99,100,setOf("a"))
  valid()
  assertTrue(runCatching{valid(pkg="other.app")}.isFailure)
  assertTrue(runCatching{valid(signers=setOf("b"))}.isFailure)
  assertTrue(runCatching{valid(version=98)}.isFailure)
  assertTrue(runCatching{valid(version=101)}.isFailure)
 }
 @Test fun validatesActualSignedApkAndRejectsGarbage(){
  val installed=File(c.applicationInfo.sourceDir)
  ApkDownloads.validateFile(c,installed,BuildConfig.VERSION_CODE)
  assertTrue(runCatching{ApkDownloads.validateFile(c,installed,BuildConfig.VERSION_CODE+1)}.isFailure)
  val bad=File(c.cacheDir,"bad-update-test.apk")
  try {bad.writeText("not an apk");assertTrue(runCatching{ApkDownloads.validateFile(c,bad,BuildConfig.VERSION_CODE)}.isFailure)} finally{bad.delete()}
 }
 @Test fun downloadSurvivesReopenAndCanBeInstalled(){
  val url=InstrumentationRegistry.getArguments().getString("apkDownloadUrl")
  org.junit.Assume.assumeTrue(url!=null)
  c.startActivity(android.content.Intent(c,MainActivity::class.java).addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK))
  Thread.sleep(1000)
  val requireVpn=InstrumentationRegistry.getArguments().getString("apkRequireVpn")=="true"
  val cm=c.getSystemService(android.net.ConnectivityManager::class.java)
  fun vpns()=cm.allNetworks.filter{cm.getNetworkCapabilities(it)?.hasTransport(android.net.NetworkCapabilities.TRANSPORT_VPN)==true}.toSet()
  if(requireVpn){
   c.startForegroundService(android.content.Intent(c,LabVpnService::class.java))
   val ready=System.currentTimeMillis()+30000
   while(vpns().isEmpty() && System.currentTimeMillis()<ready)Thread.sleep(250)
   assertTrue("VPN must be up before download",vpns().isNotEmpty())
  }
  val vpnBefore=vpns()
  ApkDownloads.cancel(c)
  val offer=ApplicationUpdates.Offer("acceptance","quic-lab",BuildConfig.VERSION_NAME,BuildConfig.VERSION_CODE,url!!)
  ApkDownloads.start(c,offer)
  val id=ApkDownloads.snapshot(c).id
  ApkDownloads.start(c,offer)
  assertEquals(id,ApkDownloads.snapshot(c).id)
  val until=System.currentTimeMillis()+180000
  var state=ApkDownloads.snapshot(c)
  while(state.status!=android.app.DownloadManager.STATUS_SUCCESSFUL && state.status!=android.app.DownloadManager.STATUS_FAILED && System.currentTimeMillis()<until){Thread.sleep(500);state=ApkDownloads.snapshot(c)}
  assertEquals(state.message,android.app.DownloadManager.STATUS_SUCCESSFUL,state.status)
  if(requireVpn)assertEquals("VPN must not be restarted to download",vpnBefore,vpns())
  val nm=c.getSystemService(android.app.NotificationManager::class.java)
  val noticeUntil=System.currentTimeMillis()+10000
  while(nm.activeNotifications.none{it.id==ApkDownloads.NOTIFICATION} && System.currentTimeMillis()<noticeUntil)Thread.sleep(200)
  assertTrue("Completed transfer must notify",nm.activeNotifications.any{it.id==ApkDownloads.NOTIFICATION})
  val file=ApkDownloads.prepareInstaller(c)
  assertTrue(file.isFile)
  val intent=ApkDownloads.installerIntent(c,file)
  assertEquals("content",intent.data!!.scheme)
  assertEquals("application/vnd.android.package-archive",intent.type)
  assertNotEquals(0,intent.flags and android.content.Intent.FLAG_GRANT_READ_URI_PERMISSION)
 }
 @Test fun interruptedServiceOffersRetry(){
  val args=InstrumentationRegistry.getArguments()
  val stage=args.getString("apkDeathStage")
  org.junit.Assume.assumeTrue(stage!=null)
  c.startActivity(android.content.Intent(c,MainActivity::class.java).addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK))
  Thread.sleep(1000)
  if(stage=="prepare"){
   ApkDownloads.cancel(c)
   ApkDownloads.start(c,ApplicationUpdates.Offer("","",BuildConfig.VERSION_NAME,BuildConfig.VERSION_CODE,args.getString("apkSlowUrl")!!))
   val until=System.currentTimeMillis()+20000
   while(ApkDownloads.snapshot(c).bytes==0L && ApkDownloads.snapshot(c).active && System.currentTimeMillis()<until)Thread.sleep(100)
   assertTrue("Slow download must be running before runner terminates process",ApkDownloads.snapshot(c).active)
   assertTrue(ApkDownloads.snapshot(c).bytes>0)
  }else{
   val interrupted=ApkDownloads.snapshot(c)
   assertEquals(android.app.DownloadManager.STATUS_FAILED,interrupted.status)
   assertTrue(interrupted.message,interrupted.message.contains("Повторить"))
   ApkDownloads.retry(c)
   val until=System.currentTimeMillis()+180000
   while(ApkDownloads.snapshot(c).active && System.currentTimeMillis()<until)Thread.sleep(500)
   assertEquals(ApkDownloads.snapshot(c).message,android.app.DownloadManager.STATUS_SUCCESSFUL,ApkDownloads.snapshot(c).status)
  }
 }
 @Test fun liveHttpFailureRetryAndCancel(){
  val url=InstrumentationRegistry.getArguments().getString("apkDownloadUrl")
  org.junit.Assume.assumeTrue(url!=null)
  c.startActivity(android.content.Intent(c,MainActivity::class.java).addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK))
  Thread.sleep(1000)
  val isolated=c
  ApkDownloads.cancel(c)
  try {
   ApkDownloads.start(isolated,ApplicationUpdates.Offer("","",BuildConfig.VERSION_NAME,BuildConfig.VERSION_CODE,url+"-missing"))
   val first=ApkDownloads.snapshot(isolated).id
   val until=System.currentTimeMillis()+30000
   var state=ApkDownloads.snapshot(isolated)
   while(state.active && System.currentTimeMillis()<until){Thread.sleep(250);state=ApkDownloads.snapshot(isolated)}
   assertEquals(android.app.DownloadManager.STATUS_FAILED,state.status)
   assertTrue(state.message,state.message.contains("404"))
   ApkDownloads.retry(isolated)
   assertNotEquals(first,ApkDownloads.snapshot(isolated).id)
   ApkDownloads.cancel(isolated)
   assertFalse(ApkDownloads.hasDownload(isolated))
  }finally{ApkDownloads.cancel(isolated)}
 }

}
