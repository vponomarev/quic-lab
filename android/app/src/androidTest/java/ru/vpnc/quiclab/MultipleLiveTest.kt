package ru.vpnc.quiclab

import android.content.Context
import android.content.Intent
import android.content.SharedPreferences
import android.net.VpnService
import android.os.ParcelFileDescriptor
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test

class MultipleLiveTest {
    @Test
    fun realUidRoutesPauseAndPriority() {
        val inst = InstrumentationRegistry.getInstrumentation()
        val args = InstrumentationRegistry.getArguments()
        assumeTrue(args.getString("multiple_live") == "true")
        val c = inst.targetContext
        val target = args.getString("probe_host") ?: error("probe_host is required")
        require(target.matches(Regex("[0-9.]+"))) { "probe_host must be an IPv4 address" }
        val selected = "ru.vpnc.quicprobe"
        val direct = "$selected.direct"
        assertFalse("Stop VPN before live test", LabVpnService.active)
        val meta = c.getSharedPreferences("vpn_profiles", Context.MODE_PRIVATE)
        val saved = meta.all.toMap()
        val created = mutableListOf<String>()
        fun restore(p: SharedPreferences, values: Map<String, *>) {
            val e = p.edit().clear()
            values.forEach { (k, v) ->
                when (v) {
                    is String -> e.putString(k, v)
                    is Boolean -> e.putBoolean(k, v)
                    is Int -> e.putInt(k, v)
                    is Long -> e.putLong(k, v)
                    is Float -> e.putFloat(k, v)
                    is Set<*> -> e.putStringSet(k, v.filterIsInstance<String>().toSet())
                }
            }
            check(e.commit())
        }
        fun shell(cmd: String): String =
            inst.uiAutomation.executeShellCommand(cmd).use {
                ParcelFileDescriptor.AutoCloseInputStream(it).bufferedReader().readText()
            }
        fun waitFor(label: String, seconds: Int = 45, predicate: () -> Boolean) {
            val end = System.currentTimeMillis() + seconds * 1000
            while (!predicate() && System.currentTimeMillis() < end) Thread.sleep(200)
            assertTrue("$label: ${MultipleVpnState.summary()}", predicate())
        }
        fun stop() {
            c.startService(Intent(c, LabVpnService::class.java).setAction("stop"))
            waitFor("stopped", 10) { !LabVpnService.active }
            Thread.sleep(1200)
        }
        fun start() {
            c.startActivity(
                Intent(c, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            )
            Thread.sleep(700)
            assertNull("VPN permission required", VpnService.prepare(c))
            c.startForegroundService(Intent(c, LabVpnService::class.java))
            waitFor("two live profiles") { MultipleVpnState.summary().split("Работает").size >= 3 }
        }
        fun probe(pkg: String, kind: String, host: String = target, success: Boolean = true) {
            val id = "p" + System.nanoTime()
            shell("am force-stop $pkg")
            shell(
                "am start -W -n $pkg/ru.vpnc.quicprobe.ProbeActivity --es id $id --es kind $kind --es host $host --ei port 39081"
            )
            var raw = ""
            waitFor("probe result", 15) {
                raw = shell("run-as $pkg cat files/$id.json")
                raw.trim().startsWith("{")
            }
            val r = JSONObject(raw)
            println(
                "PROBE $pkg $kind success=${r.optBoolean("ok")} uid=${r.optInt("uid")} error=${r.optString("error")}"
            )
            assertEquals("$pkg $kind: $raw", success, r.optBoolean("ok"))
            shell("run-as $pkg rm files/$id.json")
        }
        try {
            val candidates = listOf(VpnProfiles.current(c)) + VpnProfiles.list(c)
            val source =
                candidates.firstOrNull { p ->
                    val prefs = VpnProfiles.preferences(c, p.id)
                    !prefs.getString("quic_endpoint", "").isNullOrBlank() &&
                        !prefs.getString("https_endpoint", "").isNullOrBlank() &&
                        VpnProfiles.identityFile(c, p.id).exists()
                } ?: error("A saved QUIC/HTTPS profile is required")
            val template = VpnProfiles.preferences(c, source.id).all.toMap()
            fun clone(name: String, transport: String, mode: Int): VpnProfiles.Profile {
                val p = VpnProfiles.create(c, name)
                created.add(p.id)
                restore(VpnProfiles.preferences(c, p.id), template)
                VpnProfiles.identityFile(c, source.id)
                    .copyTo(VpnProfiles.identityFile(c, p.id), overwrite = true)
                val prefs = VpnProfiles.preferences(c, p.id)
                check(
                    prefs
                        .edit()
                        .putString("transport", transport)
                        .putString("endpoint", prefs.getString("${transport}_endpoint", ""))
                        .putInt("mode", mode)
                        .putString("routes", "$target/32")
                        .putBoolean("global_apps", false)
                        .putStringSet("apps", setOf(selected))
                        .commit()
                )
                return p
            }
            val home = clone("Multiple test · subnet", "https", 3)
            val apps = clone("Multiple test · apps", "quic", 1)
            VpnProfiles.setEnabled(c, setOf(home.id, apps.id))
            VpnProfiles.setMultiple(c, true)
            VpnProfiles.setDnsProfile(c, apps.id)
            println("PROFILES home=${home.id} apps=${apps.id}")
            start()
            probe(selected, "tcp")
            probe(selected, "udp")
            probe(selected, "https", "api.ipify.org")
            probe(direct, "tcp")
            probe(direct, "https", "api.ipify.org")
            val report = Diagnostics.report(c, "")
            val uid = c.packageManager.getApplicationInfo(selected, 0).uid
            assertTrue(
                "App UID routed",
                report.contains("profile_id=${apps.id} reason=apps uid=$uid"),
            )
            assertTrue(
                "Subnet priority",
                report.contains("profile_id=${home.id} reason=subnet uid=$uid"),
            )
            assertTrue("Unmatched direct", report.contains("profile_id=direct reason=unmatched"))
            c.startService(
                Intent(c, LabVpnService::class.java)
                    .setAction("toggle-profile")
                    .putExtra("profile_id", home.id)
            )
            Thread.sleep(1200)
            probe(selected, "tcp", success = false)
            probe(selected, "udp", success = false)
            probe(selected, "https", "api.ipify.org")
            c.startService(
                Intent(c, LabVpnService::class.java)
                    .setAction("toggle-profile")
                    .putExtra("profile_id", home.id)
            )
            waitFor("resumed") { MultipleVpnState.summary().split("Работает").size >= 3 }
            probe(selected, "udp")
            stop()
            VpnProfiles.move(c, apps.id, -1)
            start()
            probe(selected, "tcp")
            probe(selected, "udp")
            probe(direct, "udp")
            val reordered = Diagnostics.report(c, "")
            assertTrue(
                "App priority after reorder",
                reordered.lineSequence().any {
                    it.contains("profile_id=${apps.id} reason=apps uid=$uid") &&
                        it.contains("destination=$target")
                },
            )
            shell("am start -W -n ru.vpnc.quiclab/.MainActivity")
            Thread.sleep(500)
            inst.uiAutomation.takeScreenshot()?.let { img ->
                File(c.filesDir, "multiple-live.png").outputStream().use {
                    img.compress(android.graphics.Bitmap.CompressFormat.PNG, 100, it)
                }
                img.recycle()
            }
            println("MULTIPLE_LIVE_OK")
        } finally {
            File(c.filesDir, "multiple-live-report.txt")
                .writeText(Diagnostics.report(c, "multiple live integration"))
            stop()
            created.forEach {
                VpnProfiles.preferences(c, it).edit().clear().commit()
                VpnProfiles.identityFile(c, it).delete()
            }
            restore(meta, saved)
            shell("am force-stop $selected")
            shell("am force-stop $direct")
        }
    }
}
