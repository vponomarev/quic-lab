package ru.vpnc.quiclab

import org.json.JSONObject
import android.content.Context
import android.content.ContextWrapper
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.UUID
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class ProfileCompatibilityTest {
    private fun profile(): JSONObject {
        val key = android.util.Base64.encodeToString(ByteArray(32) { 1 }, android.util.Base64.NO_WRAP)
        val awg = "[Interface]\nPrivateKey = $key\nAddress = 10.20.0.2\nDNS = 1.1.1.1\n[Peer]\nPublicKey = $key\nAllowedIPs = 0.0.0.0/0\nEndpoint = vpn.example:52000\n"
        return JSONObject().put("version", 2).put("kind", "vpn")
            .put("endpoint", "lab.example:4433").put("hostname", "lab.example")
            .put("transports", org.json.JSONArray().put("awg")).put("awg_config", awg)
    }

    private class IsolatedContext(base: Context) : ContextWrapper(base) {
        private val prefix = "compatibility-${UUID.randomUUID()}-"
        private val names = mutableSetOf<String>()
        private val dir = File(base.cacheDir, prefix).apply { mkdirs() }
        override fun getFilesDir() = dir
        override fun getSharedPreferences(name: String, mode: Int) =
            baseContext.getSharedPreferences(prefix + name, mode).also { names.add(prefix + name) }
        fun close() {
            names.forEach { baseContext.deleteSharedPreferences(it) }
            dir.deleteRecursively()
        }
    }

    @Test fun legacyProfileStillValidatesWithoutCompatibilityFields() {
        assertEquals("vpn", ProfileImport.validate(profile()))
    }

    @Test fun explicitServerNameRequiresVerifyName() {
        val p = profile().put("server_name", "cover.example")
        try {
            ProfileImport.validate(p)
            assertTrue("explicit server_name without verify_name must fail", false)
        } catch (_: IllegalArgumentException) {
            // expected
        }
    }

    @Test fun explicitCompatibilityFieldsAndControlUrlValidate() {
        val p = profile()
            .put("server_name", "cover.example")
            .put("verify_name", "lab.example")
            .put("control_url", "https://lab.example/capabilities")
            .put("data_version", 1)
        assertEquals("vpn", ProfileImport.validate(p))
    }

    @Test fun importSaveRoundTripsCompatibilityFields() {
        val base = InstrumentationRegistry.getInstrumentation().targetContext
        val c = IsolatedContext(base)
        try {
            val key = android.util.Base64.encodeToString(ByteArray(32) { 1 }, android.util.Base64.NO_WRAP)
            val awg = "[Interface]\nPrivateKey = $key\nAddress = 10.20.0.2\nDNS = 1.1.1.1\n[Peer]\nPublicKey = $key\nAllowedIPs = 0.0.0.0/0\nEndpoint = vpn.example:52000\n"
            val p = JSONObject().put("version", 2).put("kind", "vpn").put("hostname", "lab.example")
                .put("server_name", "cover.example").put("verify_name", "lab.example")
                .put("control_url", "https://lab.example/capabilities").put("data_version", 1)
                .put("transports", org.json.JSONArray().put("awg")).put("awg_config", awg)
            assertEquals("vpn", ProfileImport.save(c, p))
            val prefs = VpnProfiles.preferences(c)
            assertEquals("cover.example", prefs.getString("server_name", null))
            assertEquals("lab.example", prefs.getString("verify_name", null))
            assertEquals("https://lab.example/capabilities", prefs.getString("control_url", null))
            assertEquals(1, prefs.getInt("data_version", 0))
            val local = VpnConfiguration.load(c).getJSONObject("local").getJSONObject("profiles")
                .getJSONObject(VpnProfiles.current(c).id)
            assertEquals("cover.example", local.getString("server_name"))
            assertEquals(1, local.getInt("data_version"))
            c.getSharedPreferences("vpn_profiles", 0).edit()
                .putBoolean("multiple", true)
                .putStringSet("multiple_enabled", setOf(VpnProfiles.current(c).id)).commit()
            val multiple = MultipleVpnPlan.load(c).profiles.single().config
            assertEquals("cover.example", multiple.getString("server_name"))
            assertEquals(1, multiple.getInt("data_version"))
        } finally {
            c.close()
        }
    }

    @Test fun controlUrlRejectsCredentialsQueryAndFragment() {
        for (url in listOf(
            "http://lab.example/capabilities",
            "https://user@lab.example/capabilities",
            "https://lab.example/capabilities?x=1",
            "https://lab.example/capabilities#frag",
            "https://lab.example:0/capabilities",
        )) {
            try {
                ProfileImport.validate(profile().put("control_url", url))
                assertTrue("invalid control_url accepted: $url", false)
            } catch (_: IllegalArgumentException) {
                // expected
            }
        }
        assertEquals("vpn", ProfileImport.validate(profile().put("control_url", "")))
    }
}
