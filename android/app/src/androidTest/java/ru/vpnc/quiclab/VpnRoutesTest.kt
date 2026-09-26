package ru.vpnc.quiclab

import org.junit.Assert.*
import org.junit.Test

class VpnRoutesTest {
    @Test
    fun normalizesNetworksAndPreservesHostRoutes() {
        assertEquals(
            listOf("192.168.50.0" to 24, "10.0.0.8" to 32),
            VpnRoutes.parse("192.168.50.19/24\n10.0.0.8/32"),
        )
    }

    @Test
    fun rejectsInvalidOrEmptyRoutes() {
        for (value in listOf("", "::/0", "10.0.0.1/33", "999.0.0.1/8", "example.org/24")) {
            try {
                VpnRoutes.parse(value)
                fail(value)
            } catch (_: IllegalArgumentException) {}
        }
    }
}
