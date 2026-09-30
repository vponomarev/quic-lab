package ru.vpnc.quiclab

import android.content.Intent
import android.os.PowerManager
import android.os.SystemClock
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

class VpnRttTest {
    @Test fun screenPolicyAndLiveTunnel() {
        val inst=InstrumentationRegistry.getInstrumentation()
        val args=InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("vpn_rtt")=="true")
        val context=inst.targetContext
        assumeTrue(!VpnProfiles.multiple(context))
        val settings=VpnRttSettings.preferences(context)
        val hadOn=settings.contains("screen_on");val hadOff=settings.contains("screen_off")
        val oldOn=settings.getLong("screen_on",1000);val oldOff=settings.getLong("screen_off",0)
        val profile=VpnProfiles.preferences(context)
        val oldTransport=profile.getString("transport","quic")!!
        val oldEndpoint=profile.getString("endpoint","")!!
        val transport=args.getString("transport",oldTransport)!!
        val endpoint=profile.getString("${transport}_endpoint",if(transport==oldTransport)oldEndpoint else "").orEmpty()
        assumeTrue("No configured endpoint for $transport",endpoint.isNotBlank())
        fun shell(command:String) { inst.uiAutomation.executeShellCommand(command).use { android.os.ParcelFileDescriptor.AutoCloseInputStream(it).readBytes() } }
        fun waitFor(label:String,timeout:Long=15000,predicate:()->Boolean) {
            val until=SystemClock.elapsedRealtime()+timeout
            while(SystemClock.elapsedRealtime()<until && !predicate())Thread.sleep(100)
            assertTrue("$label: ${LabVpnService.status}",predicate())
        }
        try {
            shell("input keyevent 224")
            shell("am start -W -n ru.vpnc.quiclab/.MainActivity")
            assertNull("VPN consent required",android.net.VpnService.prepare(context))
            context.stopService(Intent(context,LabVpnService::class.java))
            waitFor("stop prior VPN") {!LabVpnService.active}
            Thread.sleep(1000)
            profile.edit().putString("transport",transport).putString("endpoint",endpoint).commit()
            settings.edit().putLong("screen_on",1000).putLong("screen_off",0).commit()
            context.startForegroundService(Intent(context,LabVpnService::class.java))
            waitFor("initial RTT",30000) {LabVpnService.active && LabVpnService.lastEcho>0}
            Thread.sleep(12000) // Let the initial cellular-to-WiFi preference settle.
            val initialSession=LabVpnService.connection
            shell("input keyevent 223")
            waitFor("screen off") {!context.getSystemService(PowerManager::class.java).isInteractive && !LabVpnService.rttEnabled}
            Thread.sleep(1000)
            val health=LabVpnService.lastHealth
            waitFor("health with RTT disabled") {LabVpnService.lastHealth>health}
            assertEquals(0L,LabVpnService.lastEcho)
            assertEquals(0L,LabVpnService.lastTransitEcho)
            assertTrue(LabVpnService.notificationSnapshot(SystemClock.elapsedRealtime()).toString().contains("выключен"))
            assertEquals("No reconnect on screen off",initialSession,LabVpnService.connection)
            settings.edit().putLong("screen_off",5000).commit()
            waitFor("screen-off setting applied live") {LabVpnService.rttEnabled && LabVpnService.rttInterval==5000L && LabVpnService.lastEcho>0}
            shell("input keyevent 224")
            waitFor("screen-on setting restored") {LabVpnService.rttEnabled && LabVpnService.rttInterval==1000L && LabVpnService.lastEcho>0}
            assertEquals("No reconnect on screen on",initialSession,LabVpnService.connection)
            settings.edit().putLong("screen_on",0).commit()
            waitFor("disable while screen on") {!LabVpnService.rttEnabled && LabVpnService.lastEcho==0L}
            println("RTT policy PASS: $transport; on/off/live settings; health preserved; no reconnect")
        } finally {
            shell("input keyevent 224")
            context.stopService(Intent(context,LabVpnService::class.java))
            Thread.sleep(1000)
            profile.edit().putString("transport",oldTransport).putString("endpoint",oldEndpoint).commit()
            settings.edit().apply {if(hadOn)putLong("screen_on",oldOn) else remove("screen_on");if(hadOff)putLong("screen_off",oldOff) else remove("screen_off")}.commit()
        }
    }
}
