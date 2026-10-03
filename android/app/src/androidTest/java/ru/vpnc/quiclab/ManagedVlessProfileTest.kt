package ru.vpnc.quiclab

import android.content.Context
import android.content.ContextWrapper
import android.content.SharedPreferences
import androidx.test.platform.app.InstrumentationRegistry
import java.io.File
import java.util.UUID
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeFalse
import org.junit.Test

class ManagedVlessProfileTest {
    @Test fun qrReviewShowsMixedEndpointsWithoutVlessCredentials() {
        val p=profile(true).put("quic","vpn.example:443").put("https","vpn.example:443")
        p.put("transports",JSONArray().put("quic").put("https").put("vless").put("awg"))
        val review=ProfileImport.reviewAddress(p)
        assertTrue(review.contains("QUIC: vpn.example:443"))
        assertTrue(review.contains("HTTPS: vpn.example:443"))
        assertTrue(review.contains("VLESS: vless.example:443"))
        assertTrue(review.contains("AmneziaWG: awg.example:52000"))
        assertFalse(review.contains(uuid))
        assertFalse(review.contains("vless://"))
    }
    private val device = "22222222-2222-4222-8222-222222222222"
    private val uuid = "11111111-1111-4111-8111-111111111111"
    private fun uri(host: String = "vless.example", id: String = uuid) =
        "vless://$id@$host:443?security=tls&type=tcp&sni=server.example&fp=chrome#Managed"
    private fun awg(): String {
        val key = android.util.Base64.encodeToString(ByteArray(32) { 1 }, android.util.Base64.NO_WRAP)
        return "[Interface]\nPrivateKey = $key\nAddress = 10.20.0.2\nDNS = 1.1.1.1\n[Peer]\nPublicKey = $key\nAllowedIPs = 0.0.0.0/0\nEndpoint = awg.example:52000\n"
    }
    private fun profile(mixed: Boolean = false): JSONObject = JSONObject()
        .put("version", 2).put("kind", "vpn").put("hostname", "vpn.example")
        .put("transports", JSONArray().put("vless").apply { if (mixed) put("awg") })
        .put("vless_uri", uri()).put("dns", "1.1.1.1")
        .put("device_id", device).put("update_token", "a".repeat(64))
        .put("config_url", "https://control.example/api/v1/devices/$device/config")
        .apply { if (mixed) put("awg_config", awg()) }
    private fun envelope(server: JSONObject, revision: Long): JSONObject {
        val config = JSONObject(server.toString()).apply { remove("update_token"); remove("device_id") }
        return JSONObject().put("schema_version", 1).put("config_revision", revision)
            .put("device_id", device).put("exit_id", device).put("server_config", config)
            .put("capabilities", JSONObject().put("control_version", 1).put("data_version", 1)
                .put("min_android_version_code", 0))
    }
    private class IsolatedContext(base: Context) : ContextWrapper(base) {
        private val prefix = "managed-vless-test-${UUID.randomUUID()}"
        private val names = mutableSetOf<String>()
        private val directory = File(base.cacheDir, prefix).apply { check(mkdirs()) }
        override fun getFilesDir(): File = directory
        override fun getSharedPreferences(name: String, mode: Int): SharedPreferences {
            names.add(prefix + name)
            return baseContext.getSharedPreferences(prefix + name, mode)
        }
        fun clean() {
            names.forEach { baseContext.deleteSharedPreferences(it) }
            check(directory.parentFile == baseContext.cacheDir)
            directory.deleteRecursively()
        }
    }
    private fun withContext(block: (Context) -> Unit) {
        assumeFalse(LabVpnService.active)
        val c = IsolatedContext(InstrumentationRegistry.getInstrumentation().targetContext)
        try { block(c) } finally { c.clean() }
    }
    private fun rejected(block: () -> Unit) {
        try { block(); fail("Unsupported managed profile accepted") } catch (_: Exception) { }
    }
    private fun local(c: Context, id: String) = c.getSharedPreferences("vpn_$id", 0)
    private fun assertPrivate(c: Context, id: String) {
        assertFalse(local(c, id).all.toString().contains(uuid))
        assertFalse(local(c, id).contains("vless_uri"))
        assertFalse(local(c, id).contains("vless_config"))
        val snapshot = VpnConfiguration.load(c).toString()
        assertFalse(snapshot.contains(uuid)); assertFalse(snapshot.contains("vless_uri"))
        assertFalse(snapshot.contains("vless_config"))
        assertFalse(VpnProfiles.identityFile(c, id).readBytes().toString(Charsets.ISO_8859_1).contains(uuid))
    }
    @Test fun managedImportStoresCanonicalEncryptedCredentialsAndEndpointOnly() = withContext { c ->
        assertEquals("vpn", ProfileImport.save(c, profile()))
        val id = VpnProfiles.current(c).id
        val bundle = VpnIdentity.load(c, id)
        assertEquals(mobile.Mobile.importVLESSConfig(uri()), bundle.getString("vless_config"))
        assertEquals(device, bundle.getString("device_id"))
        val p = VpnProfiles.preferences(c, id)
        assertTrue(p.getBoolean("managed_profile", false))
        assertEquals("vless", p.getString("transport", null))
        assertEquals("vless.example:443", p.getString("endpoint", null))
        assertEquals("vless.example:443", p.getString("vless_endpoint", null))
        assertPrivate(c, id)
    }
    @Test fun mixedTransportImportAndFourTransportValidation() = withContext { c ->
        val input = profile(true)
        ProfileImport.save(c, input)
        val bundle = VpnIdentity.load(c)
        assertTrue(bundle.has("vless_config")); assertTrue(bundle.has("awg_config"))
        assertEquals(setOf("vless", "awg"), VpnProfiles.preferences(c).getStringSet("available_transports", null))
        val four = profile(true).put("transports", JSONArray().put("quic").put("https").put("awg").put("vless"))
            .put("quic", "quic.example:443").put("https", "https.example:8443")
            .put("certificate", "placeholder").put("key", "placeholder")
        assertEquals("vpn", ProfileImport.validate(four))
    }
    @Test fun unsupportedImportIsRejectedBeforeCreatingOrSavingProfiles() = withContext { c ->
        val before = VpnProfiles.list(c)
        val selected = VpnProfiles.current(c).id
        for (input in listOf(profile().put("vless_uri", uri().replace("#Managed", "&unsupported=secret#Managed")),
            profile().put("vless_uri", uri().replace("type=tcp", "type=ws")),
            profile().put("vless_uri", JSONObject().put("outbounds", JSONArray()).toString()),
            profile().put("vless_uri", uri() + "\n" + uri()),
            profile().apply { remove("vless_uri") },
            profile().put("transports", JSONArray().put("awg")).put("awg_config", awg()),
            profile().put("transports", JSONArray().put("vless").put("vless")))) {
            rejected { ProfileImport.save(c, input) }
            assertEquals(before, VpnProfiles.list(c)); assertEquals(selected, VpnProfiles.current(c).id)
            assertFalse(VpnProfiles.identityFile(c).exists())
        }
    }
    @Test fun updatePreservesLocalSelectionRoutesApplicationsBudgetsAndDemuxPreference() = withContext { c ->
        ProfileImport.save(c, profile(true)); val id = VpnProfiles.current(c).id
        val raw = local(c, id)
        raw.edit().putString("transport", "awg").putString("routes", "192.168.1.0/24").putInt("mode", 3)
            .putStringSet("apps", setOf("example.app")).putBoolean("global_apps", false)
            .putLong("bond_copy_kib", 17).putLong("cell_mib", 19)
            .putBoolean("demux_enabled", true).putBoolean("max_availability", true).commit()
        val before = raw.all.toMap()
        val incoming = envelope(profile(true).put("vless_uri", uri("new-vless.example")), 1)
        ProfileUpdate.apply(c, id, incoming)
        assertEquals(before, raw.all)
        val bundle = VpnIdentity.load(c, id)
        assertEquals(mobile.Mobile.importVLESSConfig(uri("new-vless.example")), bundle.getString("vless_config"))
        assertEquals(1L, bundle.getJSONObject("update_envelope").getLong("config_revision"))
        assertEquals("awg", VpnProfiles.preferences(c, id).getString("transport", null))
        raw.edit().putString("transport", "vless").commit()
        val effective = VpnProfiles.preferences(c, id)
        assertEquals("new-vless.example:443", effective.getString("endpoint", null))
        assertFalse(effective.getBoolean("demux_enabled", true))
        assertEquals(false, effective.all["demux_enabled"])
        assertTrue(raw.getBoolean("demux_enabled", false))
        val model = VpnConfiguration.load(c).getJSONObject("model")
        assertEquals("standalone", model.getJSONArray("exits").getJSONObject(1).getString("kind"))
        assertEquals("vless", model.getJSONArray("profiles").getJSONObject(1).getString("transport"))
        raw.edit().putString("transport", "awg").commit()
        assertTrue(VpnProfiles.preferences(c, id).getBoolean("demux_enabled", false))
        assertPrivate(c, id)
    }
    @Test fun invalidUpdateAndAtomicWriteFailureKeepCredentialsRevisionAndLocalPreferences() = withContext { c ->
        ProfileImport.save(c, profile()); val id = VpnProfiles.current(c).id
        ProfileUpdate.apply(c, id, envelope(profile(), 1))
        val file = VpnProfiles.identityFile(c, id); val before = file.readBytes(); val prefs = local(c, id).all.toMap()
        rejected { ProfileUpdate.apply(c, id, envelope(profile().put("vless_uri", uri().replace("type=tcp", "type=xhttp")), 2)) }
        assertArrayEquals(before, file.readBytes()); assertEquals(prefs, local(c, id).all)
        val blocker = File(file.path + ".new").apply { check(mkdirs()) }
        File(blocker, "block").writeText("block")
        try {
            try { ProfileUpdate.apply(c, id, envelope(profile().put("vless_uri", uri("changed.example")), 2)); fail("Write should fail") }
            catch (_: java.io.IOException) { }
            assertArrayEquals(before, file.readBytes()); assertEquals(prefs, local(c, id).all)
        } finally { File(blocker, "block").delete(); blocker.delete() }
        ProfileUpdate.apply(c, id, envelope(profile().put("vless_uri", uri("changed.example")), 2))
        assertEquals("changed.example:443", VpnProfiles.preferences(c, id).getString("endpoint", null))
    }
    @Test fun removedVlessTransportRemovesCanonicalSecretWithoutOverwritingLocalChoice() = withContext { c ->
        ProfileImport.save(c, profile(true)); val id = VpnProfiles.current(c).id
        val incoming = profile(true).put("transports", JSONArray().put("awg")).apply { remove("vless_uri") }
        ProfileUpdate.apply(c, id, envelope(incoming, 1))
        assertFalse(VpnIdentity.load(c, id).has("vless_config"))
        assertEquals("vless", local(c, id).getString("transport", null))
        assertEquals("awg", VpnProfiles.preferences(c, id).getString("transport", null))
        assertEquals("awg.example:52000", VpnProfiles.preferences(c, id).getString("endpoint", null))
    }
    @Test fun diffRedactsManagedUriIncludingBothUuids() {
        val current = envelope(profile(), 1)
        val changedUuid = "33333333-3333-4333-8333-333333333333"
        val incoming = envelope(profile().put("vless_uri", uri("changed.example", changedUuid)), 2)
        val diff = ProfileUpdate.diff(current, incoming)
        assertEquals("Учётные данные изменились", diff.getString("vless_uri"))
        assertFalse(diff.toString().contains(uuid)); assertFalse(diff.toString().contains(changedUuid))
        assertFalse(diff.toString().contains("vless://"))
    }
}
