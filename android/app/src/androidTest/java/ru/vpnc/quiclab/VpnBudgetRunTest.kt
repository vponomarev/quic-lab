package ru.vpnc.quiclab

import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class VpnBudgetRunTest {
    @Test fun exitRestartKeepsRunButStopStartRenewsIt() {
        val owner = VpnBudgetRun()
        val first = owner.start(100)
        assertSame(first, owner.start(100))
        val snapshot = JSONObject(first.snapshot())
        assertEquals(100L, snapshot.getLong("limit"))
        assertEquals(0L, snapshot.getLong("used"))
        owner.stop()
        assertNull(owner.current)
        val second = owner.start(100)
        assertNotEquals(snapshot.getString("epoch"), JSONObject(second.snapshot()).getString("epoch"))
        // Old asynchronous callbacks retain the old object, not the new run.
        assertEquals(snapshot.getString("epoch"), JSONObject(first.snapshot()).getString("epoch"))
    }
}
