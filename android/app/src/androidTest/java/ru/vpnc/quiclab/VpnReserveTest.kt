package ru.vpnc.quiclab

import android.content.Intent
import android.os.PowerManager
import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

class VpnReserveTest {
    @Test fun meteredPolicy() {
        assertTrue(VpnReserveSettings.permits(true, true, true, false))
        assertFalse(VpnReserveSettings.permits(true, false, true, false))
        assertTrue(VpnReserveSettings.permits(true, false, true, true))
        assertFalse(VpnReserveSettings.permits(true, true, false, true))
        assertFalse(VpnReserveSettings.permits(false, false, false, true))
        assertTrue(VpnReserveSettings.permits(false, false, true, false))
    }

    @Test fun reservePolicyAndRecovery() {
        val inst=InstrumentationRegistry.getInstrumentation()
        val args=InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("vpn_reserve")=="true")
        val context=inst.targetContext
        assumeTrue(!VpnProfiles.multiple(context))
        val reserve=VpnReserveSettings.preferences(context)
        val oldReserve=reserve.all.mapValues { it.value as Boolean }
        val wifiWasOn = context.getSystemService(android.net.wifi.WifiManager::class.java).isWifiEnabled
        val settings=VpnRttSettings.preferences(context)
        val hadOn=settings.contains("screen_on");val hadOff=settings.contains("screen_off")
        val oldOn=settings.getLong("screen_on",1000);val oldOff=settings.getLong("screen_off",0)
        val profile=VpnProfiles.preferences(context)
        val oldTransport=profile.getString("transport","quic")!!
        val oldEndpoint=profile.getString("endpoint","")!!
        val transport=args.getString("transport",oldTransport)!!
        val endpoint=profile.getString("${transport}_endpoint",if(transport==oldTransport)oldEndpoint else "").orEmpty()
        assumeTrue("No configured endpoint for $transport",endpoint.isNotBlank())
        fun shell(command:String):String = inst.uiAutomation.executeShellCommand(command).use { android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes().toString(Charsets.UTF_8) }
        fun holdsCell() = shell("dumpsys connectivity").lineSequence().any {
            it.trimStart().startsWith("uid/pid:") && it.contains("RequestorPkg: ru.vpnc.quiclab ") &&
                it.contains("[ REQUEST ") && it.contains("Transports: CELLULAR")
        }
        fun waitFor(label:String,timeout:Long=15000,predicate:()->Boolean) {
            val until=SystemClock.elapsedRealtime()+timeout
            while(SystemClock.elapsedRealtime()<until && !predicate())Thread.sleep(100)
            assertTrue("$label: ${LabVpnService.status}",predicate())
        }
        val oldStayOn = shell("settings get global stay_on_while_plugged_in").trim()
        try {
            shell("svc power stayon true")
            shell("svc wifi enable")
            shell("input keyevent 224")
            shell("am start -W -n ru.vpnc.quiclab/.MainActivity")
            assertNull("VPN consent required",android.net.VpnService.prepare(context))
            context.stopService(Intent(context,LabVpnService::class.java))
            waitFor("stop prior VPN") {!LabVpnService.active}
            Thread.sleep(1000)
            profile.edit().putString("transport",transport).putString("endpoint",endpoint).commit()
            settings.edit().putLong("screen_on",1000).putLong("screen_off",0).commit()
            reserve.edit().clear().putBoolean("metered_wifi",false).commit()
            context.startForegroundService(Intent(context,LabVpnService::class.java))
            waitFor("initial Wi-Fi",45000) {LabVpnService.active && LabVpnService.network.startsWith("Wi-Fi") && LabVpnService.lastEcho>0}
            Thread.sleep(2000)
            assertFalse("Default must not hold LTE (including idle Echo)",holdsCell())
            shell("input keyevent 224")
            reserve.edit().putBoolean("cell_on",true).commit()
            waitFor("explicit screen-on reserve holds LTE") {holdsCell()}
            shell("input keyevent 223")
            waitFor("screen-off releases LTE") {!holdsCell() && !LabVpnService.rttEnabled}
            shell("input keyevent 224")
            waitFor("screen-on reacquires enabled reserve") {holdsCell()}
            reserve.edit().putBoolean("cell_on",false).commit()
            waitFor("disable applies live") {!holdsCell()}
            shell("svc wifi disable")
            waitFor("recover on LTE despite disabled prewarm",45000) {LabVpnService.network.startsWith("LTE") && LabVpnService.lastEcho>0 && SystemClock.elapsedRealtime()-LabVpnService.lastEcho<2500}
            assertTrue("Active LTE retained", holdsCell())
            shell("svc wifi enable")
            waitFor("validated Wi-Fi preferred",45000) {LabVpnService.network.startsWith("Wi-Fi") && LabVpnService.lastEcho>0 && SystemClock.elapsedRealtime()-LabVpnService.lastEcho<2500}
            waitFor("return to Wi-Fi releases LTE") {!holdsCell()}
            println("Reserve PASS: $transport; default no LTE hold; settings and screen changes; WiFi/LTE/WiFi recovery")

        } finally {
            shell("input keyevent 224")
            shell("settings put global stay_on_while_plugged_in $oldStayOn")
            context.stopService(Intent(context,LabVpnService::class.java))
            Thread.sleep(1000)
            reserve.edit().clear().apply { oldReserve.forEach { (key,value) -> putBoolean(key,value) } }.commit()
            shell(if(wifiWasOn) "svc wifi enable" else "svc wifi disable")
            profile.edit().putString("transport",oldTransport).putString("endpoint",oldEndpoint).commit()
            settings.edit().apply {if(hadOn)putLong("screen_on",oldOn) else remove("screen_on");if(hadOff)putLong("screen_off",oldOff) else remove("screen_off")}.commit()
        }
    }
}
