package ru.vpnc.quiclab

import android.content.Intent
import android.net.VpnService
import android.os.ParcelFileDescriptor
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.io.File

class AwgTest {
    @Test fun importedIdentityIsEncryptedAndProfilesAreIsolated() {
        val context = InstrumentationRegistry.getInstrumentation().targetContext
        assumeTrue(!LabVpnService.active)
        val original = VpnProfiles.current(context).id
        val count = VpnProfiles.list(context).size
        val fake = android.util.Base64.encodeToString(ByteArray(32) { (it + 1).toByte() }, android.util.Base64.NO_WRAP)
        val raw = "[Interface]\nPrivateKey = $fake\nAddress = 10.20.0.2\nDNS = 1.1.1.1\n[Peer]\nPublicKey = $fake\nAllowedIPs = 10.20.0.0/24, ::/0\nEndpoint = vpn.example.org:52000\n"
        try {
            AwgImport.save(context, raw)
            assertEquals(count + 1, VpnProfiles.list(context).size)
            assertEquals(3, VpnProfiles.preferences(context).getInt("mode", -1))
            assertEquals(raw, VpnIdentity.load(context).getString("awg_config"))
            assertFalse(VpnProfiles.identityFile(context).readBytes().toString(Charsets.ISO_8859_1).contains(fake))
            assertFalse(VpnProfiles.preferences(context).all.toString().contains(fake))
        } finally {
            if (VpnProfiles.current(context).id != original) VpnProfiles.delete(context)
            VpnProfiles.select(context, original)
        }
    }

    @Test fun authorizedPeerLifecycleAndMigration() {
        val args = InstrumentationRegistry.getArguments()
        assumeTrue("Explicit peer/device test only", args.getString("awg_live") == "true")
        val inst = InstrumentationRegistry.getInstrumentation()
        val context = inst.targetContext
        val source = File(context.filesDir, "awg-test.conf")
        if (source.exists()) {
            val raw = source.readText()
            source.delete()
            AwgImport.save(context, raw)
        }
        assertEquals("awg", VpnProfiles.preferences(context).getString("transport", ""))
        assertTrue(VpnIdentity.load(context).has("awg_config"))
        assertFalse("Private key never stored in preferences", VpnProfiles.preferences(context).all.toString().contains("PrivateKey"))
        fun shell(command: String) = inst.uiAutomation.executeShellCommand(command).use { ParcelFileDescriptor.AutoCloseInputStream(it).readBytes() }
        fun waitFor(message: String, seconds: Int = 30, condition: () -> Boolean) {
            val until = System.currentTimeMillis() + seconds * 1000
            while (System.currentTimeMillis() < until && !condition()) Thread.sleep(100)
            assertTrue("$message: ${LabVpnService.status}", condition())
        }
        context.startActivity(Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        Thread.sleep(1500)
        VpnService.prepare(context)?.let { context.startActivity(it.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)); Thread.sleep(15000) }
        context.startForegroundService(Intent(context, LabVpnService::class.java))
        waitFor("AWG ICMP reply", 40) { LabVpnService.lastEcho >= LabVpnService.startedAt && LabVpnService.active && LabVpnService.rtt > 0 }
        waitFor("Exit IPv4 through AWG") { LabVpnService.exitIP.isNotBlank() }
        shell("am start -a android.intent.action.VIEW -d https://api.ipify.org/ -p com.android.chrome")
        waitFor("Browser traffic through TUN") { LabVpnService.txBytes > 500 && LabVpnService.rxBytes > 500 }
        if (args.getString("migrate") == "true") {
            try {
                shell("svc wifi disable")
                val before = LabVpnService.lastEcho
                waitFor("Mobile migration", 40) { LabVpnService.network.contains("Cellular") && LabVpnService.lastEcho > before + 1500 && LabVpnService.lastEcho > LabVpnService.lastTransition + 1000 }
                shell("svc wifi enable")
                val mobile = LabVpnService.lastEcho
                waitFor("Wi-Fi return", 45) { LabVpnService.network == "Wi-Fi" && LabVpnService.lastEcho > mobile + 3000 && LabVpnService.lastEcho > LabVpnService.lastTransition + 1000 }
            } finally { shell("svc wifi enable") }
        }
        repeat(2) { cycle ->
            context.startService(Intent(context, LabVpnService::class.java).setAction("stop"))
            waitFor("VPN stopped") { !LabVpnService.active }
            Thread.sleep(1500)
            assertEquals("—", LabVpnService.connection)
            if (cycle == 0) {
                context.startActivity(Intent(context, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                Thread.sleep(1000)
                val previousStart = LabVpnService.startedAt
                context.startForegroundService(Intent(context, LabVpnService::class.java))
                waitFor("AWG restart") { LabVpnService.startedAt > previousStart && LabVpnService.lastEcho >= LabVpnService.startedAt && LabVpnService.active }
            }
        }
    }
}
