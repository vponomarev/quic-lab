package ru.vpnc.quiclab

import android.content.Intent
import android.net.VpnService
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

class InnerCaptureSmokeTest {
    @Test fun existingProfileThreeTransports() {
        val args=InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("inner_capture_live")=="true")
        val c=InstrumentationRegistry.getInstrumentation().targetContext
        val host=args.getString("host") ?: error("host required")
        fun waitFor(label:String, seconds:Int=40, condition:()->Boolean) {
            val end=System.currentTimeMillis()+seconds*1000
            while(!condition() && System.currentTimeMillis()<end) Thread.sleep(100)
            assertTrue("$label: ${LabVpnService.status}",condition())
        }
        fun stop(){c.startService(Intent(c,LabVpnService::class.java).setAction("stop"));waitFor("stop",10){!LabVpnService.active};Thread.sleep(1200)}
        stop()
        val original=VpnProfiles.current(c).id
        val multiple=VpnProfiles.multiple(c)
        val target=VpnProfiles.list(c).first { p ->
            val prefs=VpnProfiles.preferences(c,p.id)
            prefs.getString("hostname","")==host && prefs.getStringSet("available_transports",emptySet())!!.containsAll(listOf("quic","https","awg"))
        }
        val prefs=VpnProfiles.preferences(c,target.id)
        val savedTransport=prefs.getString("transport","quic")
        val savedEndpoint=prefs.getString("endpoint","")
        val savedMode=prefs.getInt("mode",0)
        try {
            VpnProfiles.setMultiple(c,false);VpnProfiles.select(c,target.id)
            for(transport in listOf("quic","https","awg")) {
                prefs.edit().putInt("mode",0).putString("transport",transport).putString("endpoint",prefs.getString("${transport}_endpoint","")).commit()
                c.startActivity(Intent(c,MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                Thread.sleep(500)
                assertNull("VPN consent already required",VpnService.prepare(c))
                c.startForegroundService(Intent(c,LabVpnService::class.java))
                waitFor("$transport RTT"){LabVpnService.active && LabVpnService.rtt>0}
                waitFor("$transport tunneled exit query"){LabVpnService.exitIP.isNotBlank()}
                println("Inner capture traffic: $transport OK")
                Thread.sleep(1500)
                stop()
            }
        } finally {
            stop()
            prefs.edit().putInt("mode",savedMode).putString("transport",savedTransport).putString("endpoint",savedEndpoint).commit()
            VpnProfiles.select(c,original);VpnProfiles.setMultiple(c,multiple)
        }
    }
}
