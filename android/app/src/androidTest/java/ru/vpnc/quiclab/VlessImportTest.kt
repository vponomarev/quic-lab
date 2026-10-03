package ru.vpnc.quiclab

import androidx.test.platform.app.InstrumentationRegistry
import com.google.zxing.*
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeWriter
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeFalse
import org.junit.Test

class VlessImportTest {
    private val fixture = "vless://11111111-1111-4111-8111-111111111111@outer.invalid:443?security=reality&type=tcp&sni=server.invalid&fp=chrome&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&sid=aabb&spx=%2F#Fixture"

    @Test fun importStoresCredentialsOnlyInEncryptedBundle() {
        val c = InstrumentationRegistry.getInstrumentation().targetContext
        assumeFalse(LabVpnService.active)
        val previous = VpnProfiles.current(c).id
        val before = VpnProfiles.list(c).map { it.id }.toSet()
        try {
            val info = VlessImport.metadata(fixture)
            assertEquals("outer.invalid:443", info.getString("endpoint"))
            assertFalse(info.toString().contains("11111111"))
            // Exercise the scanner's actual QR codec before the same import/save path.
            val size = 512
            val bits = QRCodeWriter().encode(fixture, BarcodeFormat.QR_CODE, size, size)
            val pixels = IntArray(size * size) { i -> if (bits[i % size, i / size]) 0xff000000.toInt() else 0xffffffff.toInt() }
            val decoded = MultiFormatReader().decode(BinaryBitmap(HybridBinarizer(RGBLuminanceSource(size, size, pixels)))).text
            assertEquals(fixture, decoded)
            VlessImport.save(c, decoded)
            val profile = VpnProfiles.current(c)
            assertFalse(before.contains(profile.id))
            val prefs = VpnProfiles.preferences(c)
            assertEquals("vless", prefs.getString("transport", null))
            assertEquals(setOf("vless"), prefs.getStringSet("available_transports", emptySet()))
            assertEquals("outer.invalid:443", prefs.getString("vless_endpoint", null))
            assertEquals("1.1.1.1", prefs.getString("dns", null))
            assertEquals("0.0.0.0/0", prefs.getString("routes", null))
            assertFalse(prefs.all.toString().contains("11111111"))
            assertFalse(prefs.contains("vless_config"))
            val bundle = VpnIdentity.load(c)
            val canonical = JSONObject(bundle.getString("vless_config"))
            assertEquals("11111111-1111-4111-8111-111111111111", canonical.getJSONObject("config").getString("UUID"))
            assertFalse(String(VpnProfiles.identityFile(c).readBytes(), Charsets.ISO_8859_1).contains("11111111"))
        } finally {
            VpnProfiles.select(c, previous)
            VpnProfiles.list(c).filter { it.id !in before }.forEach { VpnProfiles.delete(c, it.id) }
        }
    }

    @Test fun rejectedImportPreservesProfilesAndSelection() {
        val c = InstrumentationRegistry.getInstrumentation().targetContext
        assumeFalse(LabVpnService.active)
        val previous = VpnProfiles.current(c).id
        val before = VpnProfiles.list(c)
        try {
            VlessImport.save(c, "vless://secret-invalid")
            fail("malformed VLESS accepted")
        } catch (expected: Exception) {
            assertFalse(expected.message.orEmpty().contains("secret-invalid"))
        }
        assertEquals(before, VpnProfiles.list(c))
        assertEquals(previous, VpnProfiles.current(c).id)
    }
}
