package ru.vpnc.quiclab

import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class ServerExitSelectionTest {
    @Test fun controlEndpointIsBoundToEnrolledDevice() {
        val p = JSONObject().put("device_id", "device-1").put("update_token", "a".repeat(64))
            .put("config_url", "https://updates.example/api/v1/devices/device-1/config")
        assertEquals("https://updates.example/api/v1/devices/device-1/server-exit", ServerExitSelection.endpoint(p))
        for(raw in listOf("https://updates.example/api/v1/devices/other/config", "https://updates.example/api/v1/devices/device-1/config?x=y", "http://updates.example/api/v1/devices/device-1/config")) {
            try { ServerExitSelection.endpoint(JSONObject(p.toString()).put("config_url",raw)); fail("unsafe endpoint accepted") } catch(_:IllegalArgumentException) {}
        }
    }
    @Test fun revokedChoiceRemainsVisibleAsBlockedAndCannotBecomeDefault() {
        val raw = JSONObject().put("selected","removed").put("effective","").put("legacy",false)
            .put("blocked",true).put("pending",false).put("revision","b".repeat(64)).put("choices",org.json.JSONArray())
        val view = ServerExitSelection.parse(raw.toString())
        assertEquals("removed",view.selected); assertTrue(view.blocked)
        raw.put("blocked",false)
        try { ServerExitSelection.parse(raw.toString()); fail("invalid unblocked state") } catch(_:IllegalArgumentException) {}
    }
}
