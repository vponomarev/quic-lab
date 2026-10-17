package ru.vpnc.quiclab
import androidx.test.ext.junit.runners.AndroidJUnit4
import org.junit.Test
import org.junit.Assert.*
import org.junit.runner.RunWith
@RunWith(AndroidJUnit4::class)
class VlessProbeMdmTest{
 @Test fun parsesNegotiatedProbeWithoutTreatingItAsVpnStart(){
 val parsed=MdmSyncResponse.parse("""{"version":1,"epoch":1,"serverTime":"2026-10-17T00:00:00Z","capabilities":["vless_probe_v1"],"commands":[{"id":"run1","bindingId":"binding","epoch":1,"issuedAt":"2026-10-17T00:00:00Z","expiresAt":"2026-10-17T00:05:00Z","kind":"vless_probe","probe":{"sourceId":"source","fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","action":"start"}}]}""")
 assertEquals("vless_probe",parsed.commands.single().kind)
 }
}
