package ru.vpnc.quiclab

import android.content.Intent
import android.content.IntentFilter
import android.os.BatteryManager
import android.os.Debug
import android.os.PowerManager
import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File

class PhaseOneSoakTest {
    @Test fun screenOffTcpUdp() {
        val inst = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        val seconds = args.getString("soak_seconds")?.toLong() ?: error("Explicit soak_seconds required")
        require(seconds in 60..43200)
        val host = args.getString("probe_host") ?: error("probe_host required")
        require(host.matches(Regex("[A-Za-z0-9.-]+")))
        val c = inst.targetContext
        val probe = "ru.vpnc.quicprobe"
        c.packageManager.getApplicationInfo(probe,0)
        val p = VpnProfiles.preferences(c)
        val keys = listOf("mode","global_apps","apps")
        val old = p.all.filterKeys { it in keys }
        val id = "soak" + System.currentTimeMillis()
        val output = File(c.filesDir,"$id.jsonl")
        fun shell(command:String):String = inst.uiAutomation.executeShellCommand(command).use {
            android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes().toString(Charsets.UTF_8)
        }
        fun waitFor(label:String,timeout:Long=45000,check:()->Boolean) {
            val until=SystemClock.elapsedRealtime()+timeout
            while(SystemClock.elapsedRealtime()<until&&!check())Thread.sleep(250)
            assertTrue("$label: ${LabVpnService.status}",check())
        }
        val stay = shell("settings get global stay_on_while_plugged_in").trim()
        try {
            assertFalse("Unlock phone before starting",c.getSystemService(android.app.KeyguardManager::class.java).isKeyguardLocked)
            assertFalse("Single profile required",VpnProfiles.multiple(c))
            assertNull("VPN consent required",android.net.VpnService.prepare(c))
            c.stopService(Intent(c,LabVpnService::class.java));waitFor("stop") {!LabVpnService.active}
            Thread.sleep(1000)
            p.edit().putInt("mode",1).putBoolean("global_apps",false).putStringSet("apps",setOf(probe)).commit()
            shell("input keyevent 224")
            shell("am start -a android.intent.action.MAIN -c android.intent.category.LAUNCHER -n ru.vpnc.quiclab/.MainActivity")
            c.startForegroundService(Intent(c,LabVpnService::class.java))
            waitFor("initial health",60000) {LabVpnService.active && LabVpnService.healthFresh(SystemClock.elapsedRealtime())}
            shell("am force-stop $probe")
            shell("am start -n $probe/ru.vpnc.quicprobe.ProbeActivity --es id $id --es kind soak --es host $host --el duration_ms ${seconds*1000+30000}")
            waitFor("initial TCP/UDP probe") {
                val rows=shell("run-as $probe cat files/$id.jsonl").lineSequence().filter{it.startsWith("{")}.toList()
                rows.isNotEmpty() && JSONObject(rows.last()).let {it.optBoolean("vpn")&&it.optBoolean("tcp_ok")&&it.optBoolean("udp_ok")}
            }
            shell("svc power stayon false");shell("input keyevent 223")
            waitFor("screen off") {!c.getSystemService(PowerManager::class.java).isInteractive}
            val start=SystemClock.elapsedRealtime()
            println("SOAK START id=$id seconds=$seconds utc_ms=${System.currentTimeMillis()} transport=${LabVpnService.transport}")
            var lastProbe=-1L
            var lastScreenSample=start
            var previousScreenOn=false
            var screenOnMillis=0L
            var screenOffMillis=0L
            var screenTransitions=0
            output.outputStream().bufferedWriter().use { out ->
                while(SystemClock.elapsedRealtime()-start<seconds*1000) {
                    val now=SystemClock.elapsedRealtime()
                    val screenOn=c.getSystemService(PowerManager::class.java).isInteractive
                    val interval=now-lastScreenSample
                    if(previousScreenOn) screenOnMillis+=interval else screenOffMillis+=interval
                    if(screenOn!=previousScreenOn) screenTransitions++
                    previousScreenOn=screenOn;lastScreenSample=now
                    val memory=Debug.MemoryInfo();Debug.getMemoryInfo(memory)
                    val battery=c.registerReceiver(null,IntentFilter(Intent.ACTION_BATTERY_CHANGED))
                    val raw=shell("run-as $probe cat files/$id.jsonl")
                    val rows=raw.lineSequence().filter{it.startsWith("{")}.map{JSONObject(it)}.toList()
                    assertTrue("Probe stopped or missing",rows.isNotEmpty())
                    val last=rows.last()
                    val row=JSONObject().put("elapsed_ms",now-start).put("utc_ms",System.currentTimeMillis())
                        .put("active",LabVpnService.active).put("health_age_ms",now-LabVpnService.lastHealth)
                        .put("screen_on",screenOn)
                        .put("screen_on_sampled_ms",screenOnMillis).put("screen_off_sampled_ms",screenOffMillis)
                        .put("screen_transitions_sampled",screenTransitions)
                        .put("pss_kib",memory.totalPss).put("native_heap_bytes",Debug.getNativeHeapAllocatedSize())
                        .put("java_heap_bytes",Runtime.getRuntime().totalMemory()-Runtime.getRuntime().freeMemory())
                        .put("fds",File("/proc/self/fd").list()?.size ?: -1)
                        .put("battery_level",battery?.getIntExtra(BatteryManager.EXTRA_LEVEL,-1))
                        .put("battery_plugged",battery?.getIntExtra(BatteryManager.EXTRA_PLUGGED,-1))
                        .put("probe_seq",last.getInt("seq")).put("tcp_ok",last.optBoolean("tcp_ok")).put("udp_ok",last.optBoolean("udp_ok"))
                        .put("tx_bytes",LabVpnService.txBytes).put("rx_bytes",LabVpnService.rxBytes)
                    out.write(row.toString());out.newLine();out.flush()
                    assertTrue("Unexpected VPN stop",LabVpnService.active)
                    for(r in rows.filter {it.getLong("seq")>lastProbe}) {
                        assertTrue("Probe bypassed VPN",r.getBoolean("vpn"))
                        assertTrue("TCP failure: $r",r.optBoolean("tcp_ok"))
                        assertTrue("UDP failure: $r",r.optBoolean("udp_ok"))
                    }
                    assertTrue("Probe made no progress",now-start-last.getLong("completed_ms")<60000)
                    lastProbe=last.getLong("seq")
                    Thread.sleep(10000)
                }
            }
            println("SOAK PASS id=$id elapsed_ms=${SystemClock.elapsedRealtime()-start} screen_on_sampled_ms=$screenOnMillis screen_off_sampled_ms=$screenOffMillis screen_transitions_sampled=$screenTransitions samples_file=${output.name}")
        } finally {
            shell("am force-stop $probe")
            c.stopService(Intent(c,LabVpnService::class.java));Thread.sleep(1000)
            p.edit().apply {keys.forEach{remove(it)};old.forEach{(k,v)->when(v){is String->putString(k,v);is Boolean->putBoolean(k,v);is Int->putInt(k,v);is Set<*>->putStringSet(k,v.filterIsInstance<String>().toSet())}}}.commit()
            shell("settings put global stay_on_while_plugged_in $stay")
        }
    }
}
