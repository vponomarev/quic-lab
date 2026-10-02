package ru.vpnc.quiclab

import android.content.Context
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Assert.*
import org.junit.Test

class DeviceEnrollmentTest {
    private val context = InstrumentationRegistry.getInstrumentation().targetContext
    private val raw = "https://example.invalid/lab/enroll#" + "a".repeat(64)

    @Test fun requestSurvivesResponseLossAndProcessRetry() {
        context.getSharedPreferences("enrollment_requests", Context.MODE_PRIVATE).edit().clear().commit()
        val first = ProfileImport.enrollmentRequest(context, raw)
        val retry = ProfileImport.enrollmentRequest(context, raw)
        assertEquals(first.getString("request_id"), retry.getString("request_id"))
        assertEquals("a".repeat(64), retry.getString("token"))
        assertTrue(retry.getString("device_name").isNotBlank())
        assertFalse(ProfileImport.enrollmentEndpoint(raw).toString().contains("a".repeat(64)))
        assertFalse(context.getSharedPreferences("enrollment_requests", Context.MODE_PRIVATE).all.toString().contains("a".repeat(64)))
        ProfileImport.completeEnrollment(context, raw)
        assertNotEquals(first.getString("request_id"), ProfileImport.enrollmentRequest(context, raw).getString("request_id"))
        ProfileImport.completeEnrollment(context, raw)
    }

    @Test fun rejectsEnrollmentTokenInQueryOrInsecureURL() {
        for (url in listOf("http://example.invalid/enroll#"+"a".repeat(64), "https://example.invalid/enroll?token="+"a".repeat(64), "https://example.invalid/enroll#"+"a".repeat(48))) {
            try { ProfileImport.enrollment(url);fail("invalid enrollment accepted") } catch (_: IllegalArgumentException) {}
        }
        assertEquals("https://example.invalid/lab/enroll", ProfileImport.enrollmentEndpoint(raw).toString())
        assertEquals("a".repeat(64),ProfileImport.enrollment(raw).fragment)
    }
}
