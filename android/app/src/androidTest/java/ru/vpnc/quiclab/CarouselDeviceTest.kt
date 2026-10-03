package ru.vpnc.quiclab

import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.UUID
import org.junit.Assert.*
import org.junit.Test

class CarouselDeviceTest {
 private class IsolatedContext(base:Context):ContextWrapper(base){
  private val prefix="carousel-test-"+UUID.randomUUID()
  private val names=mutableSetOf<String>()
  private val directory=File(base.cacheDir,prefix).apply{check(mkdirs())}
  override fun getFilesDir()=directory
  override fun getSharedPreferences(name:String,mode:Int):SharedPreferences {names.add(prefix+name);return baseContext.getSharedPreferences(prefix+name,mode)}
  fun clean(){names.forEach{baseContext.deleteSharedPreferences(it)};check(directory.parentFile==baseContext.cacheDir);directory.deleteRecursively()}
 }
 private fun isolated(action:(Context)->Unit){val c=IsolatedContext(InstrumentationRegistry.getInstrumentation().targetContext);try{action(c)}finally{c.clean()}}
 @Test fun demuxProjectsPersistentPerTransportPools()=isolated {c->
  c.getSharedPreferences("vpn",0).edit().putString("transport","quic").putString("endpoint","127.0.0.1:443")
   .putString("quic_endpoint","127.0.0.1:443").putString("https_endpoint","127.0.0.1:8443")
   .putBoolean("max_availability",true).putString("quic_mode","auto").putInt("quic_pool_size",3)
   .putString("https_mode","reserve").putInt("https_pool_size",2).putBoolean("https_check_reserve",true).commit()
  val first=VpnConfiguration.load(c);val profiles=first.getJSONObject("model").getJSONArray("profiles")
  assertEquals("Both authenticated transports belong to one exit",2,profiles.length())
  val quic=(0 until profiles.length()).map{profiles.getJSONObject(it)}.single{it.getString("transport")=="quic"}
  val https=(0 until profiles.length()).map{profiles.getJSONObject(it)}.single{it.getString("transport")=="https"}
  assertEquals("default",quic.getString("exit_id"));assertEquals(quic.getString("exit_id"),https.getString("exit_id"))
  assertEquals(3,quic.getInt("pool_size"));assertEquals(2,https.getInt("pool_size"));assertEquals("reserve",https.getString("mode"));assertTrue(https.getBoolean("check_reserve"))
  assertEquals(first.toString(),VpnConfiguration.load(c).toString())
 }
 @Test fun httpsDemuxCanOptIntoEconomy()=isolated {c->
  c.getSharedPreferences("vpn",0).edit().putString("transport","https").putString("endpoint","127.0.0.1:8443")
   .putBoolean("demux_enabled",true).putBoolean("max_availability",false).putInt("https_pool_size",4).commit()
  val model=VpnConfiguration.load(c).getJSONObject("model")
  assertEquals("demux",model.getJSONArray("exits").getJSONObject(0).getString("kind"))
  assertEquals(4,model.getJSONArray("profiles").getJSONObject(0).getInt("pool_size"))
 }
 @Test fun awgNeverGetsCarousel()=isolated {c->
  c.getSharedPreferences("vpn",0).edit().putString("transport","awg").putString("endpoint","127.0.0.1:51820")
   .putBoolean("demux_enabled",true).putBoolean("max_availability",true).putInt("awg_pool_size",5).commit()
  val model=VpnConfiguration.load(c).getJSONObject("model")
  assertEquals("standalone",model.getJSONArray("exits").getJSONObject(0).getString("kind"));assertEquals(1,model.getJSONArray("profiles").getJSONObject(0).getInt("pool_size"))
 }
}
