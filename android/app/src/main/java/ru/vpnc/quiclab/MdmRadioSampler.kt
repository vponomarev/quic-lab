package ru.vpnc.quiclab

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.location.LocationManager
import android.net.wifi.WifiManager
import android.os.Build
import android.os.SystemClock
import android.telephony.*
import org.json.JSONArray
import org.json.JSONObject
import java.time.Instant
import java.util.UUID
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference

/** No GPS request, Wi-Fi scan, IMEI or phone number. Serving cells only. */
internal class MdmRadioSampler(private val c:Context){
 @Suppress("MissingPermission","DEPRECATION")
 fun sample():JSONObject{
  val result=JSONObject().put("id",UUID.randomUUID().toString()).put("measuredAt",Instant.now().toString())
   .put("deviceName",Build.MANUFACTURER+" "+Build.MODEL).put("appVersion",BuildConfig.VERSION_NAME)
   .put("cells",JSONArray())
  if(c.checkSelfPermission(Manifest.permission.ACCESS_FINE_LOCATION)!=PackageManager.PERMISSION_GRANTED){
   return result.put("wifiStatus","permission_required").put("cellStatus","permission_required")
  }
  if(!c.getSystemService(LocationManager::class.java).isLocationEnabled){
   return result.put("wifiStatus","location_disabled").put("cellStatus","location_disabled")
  }
  try{
   val w=c.applicationContext.getSystemService(WifiManager::class.java).connectionInfo
   val b=w?.bssid
   if(w==null||b.isNullOrBlank()||b=="02:00:00:00:00:00"||b=="00:00:00:00:00:00"){
    result.put("wifiStatus","disconnected_or_hidden")
   }else{
    val wifi=JSONObject().put("bssid",b).put("frequencyMHz",w.frequency.coerceAtLeast(0))
    w.ssid?.takeUnless{it==WifiManager.UNKNOWN_SSID}?.let{wifi.put("ssid",it.removeSurrounding("\""))}
    if(w.rssi in -200..0)wifi.put("dbm",w.rssi)
    result.put("wifi",wifi).put("wifiStatus","available")
   }
  }catch(_:Exception){result.put("wifiStatus","unavailable")}
  val latch=CountDownLatch(1);val received=AtomicReference<List<CellInfo>?>(null)
  try{
   val manager=c.getSystemService(TelephonyManager::class.java)
   val subscription=SubscriptionManager.getDefaultDataSubscriptionId()
   val tm=if(SubscriptionManager.isValidSubscriptionId(subscription))manager.createForSubscriptionId(subscription) else manager
   tm.requestCellInfoUpdate(c.mainExecutor,object:TelephonyManager.CellInfoCallback(){
    override fun onCellInfo(cells:MutableList<CellInfo>){received.set(cells.toList());latch.countDown()}
    override fun onError(errorCode:Int,detail:Throwable?){latch.countDown()}
   })
   latch.await(8,TimeUnit.SECONDS)
   val cells=received.get()?.filter{it.isRegistered}?.take(32)
   if(cells==null)result.put("cellStatus","update_unavailable")
   else{
    val out=JSONArray();cells.forEach{cell->encode(cell)?.let{out.put(it)}}
    result.put("cells",out).put("cellStatus",if(out.length()>0)"available" else "no_serving_cell")
   }
  }catch(e:InterruptedException){Thread.currentThread().interrupt();throw e}
   catch(_:Exception){result.put("cellStatus","unavailable")}
  return result
 }
 private fun encode(cell:CellInfo):JSONObject?{
  val j=JSONObject().put("registered",cell.isRegistered)
   .put("ageMillis",(SystemClock.elapsedRealtime()-cell.timestampMillis).coerceAtLeast(0))
  fun value(key:String,n:Long){if(n>=0 && n!=Int.MAX_VALUE.toLong() && n!=Long.MAX_VALUE)j.put(key,n.toString())}
  fun operator(mcc:String?,mnc:String?){mcc?.let{j.put("mcc",it)};mnc?.let{j.put("mnc",it)}}
  when(cell){
   is CellInfoLte->cell.cellIdentity.let{j.put("technology","LTE");operator(it.mccString,it.mncString);value("ci",it.ci.toLong());value("tac",it.tac.toLong());value("pci",it.pci.toLong())}
   is CellInfoNr->(cell.cellIdentity as CellIdentityNr).let{j.put("technology","NR");operator(it.mccString,it.mncString);value("ci",it.nci);value("tac",it.tac.toLong());value("pci",it.pci.toLong())}
   is CellInfoGsm->cell.cellIdentity.let{j.put("technology","GSM");operator(it.mccString,it.mncString);value("ci",it.cid.toLong());value("tac",it.lac.toLong())}
   is CellInfoWcdma->cell.cellIdentity.let{j.put("technology","WCDMA");operator(it.mccString,it.mncString);value("ci",it.cid.toLong());value("tac",it.lac.toLong());value("pci",it.psc.toLong())}
   else->return null
  }
  cell.cellSignalStrength.dbm.takeIf{it in -200..0}?.let{j.put("dbm",it)}
  return j
 }
}
