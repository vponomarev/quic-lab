package ru.vpnc.quiclab

import org.junit.Assert.*
import org.junit.Test

class VpnUnderlyingPolicyTest {
    @Test fun selectedDefaultFollowsAndroidWithoutPinningOldWifi() {
        assertNull(VpnUnderlyingPolicy.select(listOf(1), setOf(1, 2), 1))
    }
    @Test fun lostWifiDoesNotHideLteWhileTransportWorkerIsStillRecovering() {
        assertNull(VpnUnderlyingPolicy.select(listOf(1), setOf(2), 2))
    }
    @Test fun lateTransportCallbackCannotRepublishLostWifi() {
        assertNull(VpnUnderlyingPolicy.select(listOf(1, 1), setOf(2), 2))
    }
    @Test fun deliberatelySelectedNonDefaultPathStaysExplicit() {
        assertEquals(listOf(2), VpnUnderlyingPolicy.select(listOf(2), setOf(1, 2), 1))
    }
    @Test fun multiplePathsPutLiveDefaultFirstAndRemoveDeadOnes() {
        assertEquals(listOf(2, 3), VpnUnderlyingPolicy.select(listOf(1, 3, 2, 3), setOf(2, 3), 2))
    }
    @Test fun survivingDefaultPathUsesAutomaticTracking() {
        assertNull(VpnUnderlyingPolicy.select(listOf(1, 2), setOf(2), 2))
    }
    @Test fun explicitPauseRemainsNoUnderlyingNetwork() {
        assertEquals(emptyList<Int>(), VpnUnderlyingPolicy.select(emptyList(), setOf(2), 2))
    }
    @Test fun validatedNonDefaultMayWorkEvenWithoutSystemDefault() {
        assertEquals(listOf(2), VpnUnderlyingPolicy.select(listOf(2), setOf(2), null))
    }
    @Test fun allNetworksLostDoNotRetainDeadIdUntilNextConnection() {
        assertNull(VpnUnderlyingPolicy.select(listOf(1), emptySet(), null))
    }
    @Test fun defaultChangeCannotRepinPreviousWifiBeforeWorkerCatchesUp() {
        val selection = VpnUnderlyingSelection<Int>()
        assertNull(selection.update(listOf(1), setOf(1, 2), 1))
        // Android changes its default before the transport worker processes the loss.
        assertNull(selection.refresh(setOf(1, 2), 2))
        assertNull(selection.update(listOf(1), setOf(1, 2), 2))
        // A genuinely new explicit transport choice can still select a non-default path.
        assertEquals(listOf(3), selection.update(listOf(3), setOf(2, 3), 2))
    }
    @Test fun pauseOverridesDefaultTrackingAndResumeCanChooseExplicitPath() {
        val selection = VpnUnderlyingSelection<Int>()
        assertNull(selection.update(listOf(1), setOf(1, 2), 1))
        assertEquals(emptyList<Int>(), selection.update(emptyList(), setOf(2), 2))
        assertEquals(emptyList<Int>(), selection.refresh(setOf(2, 3), 3))
        assertEquals(listOf(2), selection.update(listOf(2), setOf(2, 3), 3))
    }
    @Test fun restoredMultipathLeavesTemporaryDefaultTracking() {
        val selection = VpnUnderlyingSelection<Int>()
        assertEquals(listOf(1, 2), selection.update(listOf(1, 2), setOf(1, 2), 1))
        assertNull(selection.refresh(setOf(2), 2))
        assertEquals(listOf(2, 1), selection.refresh(setOf(1, 2), 2))
    }
    @Test fun activeBondWithoutPathsFollowsRecoveryBeforeWorkerCatchesUp() {
        val selection = VpnUnderlyingSelection<Int>()
        assertEquals(listOf(1, 2), selection.update(listOf(1, 2), setOf(1, 2), 1))
        // null is an active exit waiting for paths; [] is an intentional pause.
        assertNull(selection.update(null, emptySet(), null))
        assertNull(selection.refresh(setOf(3), 3))
        assertNull(selection.update(null, setOf(3), 3))
        assertNull(selection.update(listOf(3), setOf(3), 3))
    }
}
