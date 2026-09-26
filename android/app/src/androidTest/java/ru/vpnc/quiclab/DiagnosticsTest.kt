package ru.vpnc.quiclab

import androidx.test.platform.app.InstrumentationRegistry
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class DiagnosticsTest {
    @Test fun journalOmitsCredentialsAndPayloadSamples() {
        val c=InstrumentationRegistry.getInstrumentation().targetContext
        Diagnostics.init(c)
        Diagnostics.event("test",JSONObject().put("event","diagnostic-test").put("key","SECRET-KEY-9382").put("certificate","SECRET-CERT-9382").put("detail","password=SECRET-PASS-9382"))
        Diagnostics.event("test",JSONObject().put("event","echo").put("detail","ECHO-PAYLOAD-9382"))
        Diagnostics.event("test",JSONObject().put("event","diagnostic-test").put("error","-----BEGIN PRIVATE KEY-----SECRET-PEM-9382-----END PRIVATE KEY-----"))
        val report=Diagnostics.report(c,"sample quality metrics")
        listOf("SECRET-KEY-9382","SECRET-CERT-9382","SECRET-PASS-9382","SECRET-PEM-9382","ECHO-PAYLOAD-9382").forEach{assertFalse(report.contains(it))}
        assertTrue(report.contains("diagnostic-test"))
        assertTrue(report.contains("sample quality metrics"))
        assertFalse(Diagnostics.sanitize("https://example.test/enroll#abcdef123456").contains("abcdef123456"))
    }
}
