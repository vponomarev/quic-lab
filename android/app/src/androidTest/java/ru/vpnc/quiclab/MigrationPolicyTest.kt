package ru.vpnc.quiclab

import org.junit.Assert.*
import org.junit.Test

class MigrationPolicyTest {
    @Test fun avoidsFlappingButDetectsSilentLoss() {
        val p = MigrationPolicy()
        assertFalse(p.isStalled(300, 50.0, 50))
        assertTrue(p.isStalled(401, 50.0, 50))
        assertFalse(p.isStalled(401, 300.0, 50))
        assertTrue(p.isStalled(1201, 300.0, 50))
        assertFalse(p.isStalled(4000, 50.0, 5000))
        assertFalse(p.canPreferWifi(4900, 20000))
        assertFalse(p.canPreferWifi(6000, 7900))
        assertTrue(p.canPreferWifi(6000, 8000))
        assertEquals(2000L, p.retryDelay(1))
        assertEquals(30000L, p.retryDelay(20))
    }
}
