package ru.vpnc.quiclab

import mobile.Mobile
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class ServiceTransferTest {
    @Test fun verifiedEndpointMetadataRejectsUnsafeOriginsAndMissingAuthentication() {
        for (raw in listOf("http://updates.example/config", "https://secret@updates.example/config", "https://updates.example/config?token=secret", "https://updates.example/config#secret")) {
            try { ServiceTransfer.validateMetadata(JSONObject().put("config_url", raw).put("update_token", "synthetic")); fail("Unsafe endpoint accepted") }
            catch (_: IllegalArgumentException) { }
        }
        try { ServiceTransfer.validateMetadata(JSONObject().put("config_url", "https://updates.example/config")); fail("Anonymous config allowed") }
        catch (_: IllegalArgumentException) { }
        ServiceTransfer.validateMetadata(JSONObject().put("config_url", "https://updates.example/config").put("update_token", "synthetic"))
    }

    @Test fun unverifiedTransferCannotUseConsentToSendArbitraryTraffic() {
        val budget = Mobile.newTrafficBudget("service-test", 1)
        val transfer = Mobile.newServiceTransfer(budget, null)
        budget.grantTransfer("apk-1")
        val request = JSONObject().put("id", "apk-1").put("kind", "apk")
            .put("https_url", "https://updates.invalid/apk").put("output_file", "/unused.apk")
        try { transfer.fetch(request.toString(), budget.bind(null, "cell")); fail("No trusted profile") }
        catch (_: Exception) { }
        assertEquals(0, JSONObject(budget.snapshot()).getLong("used"))
        transfer.cancel("apk-1")
        assertTrue(budget.cellAllowed())
    }
}
