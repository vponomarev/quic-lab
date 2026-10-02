package ru.vpnc.quiclab

import android.net.LinkProperties
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.net.InetAddress

class DnsRoutingTest {
 @Test fun resolverSnapshotFiltersIPv6AndTracksLinkProperties() {
  val first=LinkProperties().apply { setDnsServers(listOf(InetAddress.getByName("192.0.2.53"), InetAddress.getByName("2001:db8::53"))) }
  assertEquals("192.0.2.53",VpnDnsPolicy.systemSnapshot(first,"wifi").getJSONArray("servers").getString(0))
  val next=LinkProperties().apply {setDnsServers(listOf(InetAddress.getByName("192.0.2.54")))}
  assertEquals("192.0.2.54",VpnDnsPolicy.systemSnapshot(next,"cell").getJSONArray("servers").getString(0))
  assertEquals(0,VpnDnsPolicy.systemSnapshot(null,"wifi").getJSONArray("servers").length())
 }
 @Test fun tunnelIsDefault() {
  val p=VpnDnsPolicy.tunnelSnapshot("home")
  assertEquals("tunnel",p.getString("mode"));assertEquals("home",p.getString("exit_id"))
 }
 @Test fun bondedResolverFollowsLossReplacementAndCellBudget() {
  val wifi=VpnSession.WIFI;val cell=VpnSession.CELLULAR
  val both=mapOf(wifi to "wifi-1",cell to "cell-1")
  assertEquals(wifi to "wifi-1",VpnDnsPolicy.selectBondResolver(both,both,true))
  val cellOnly=mapOf(cell to "cell-1")
  assertEquals(cell to "cell-1",VpnDnsPolicy.selectBondResolver(cellOnly,cellOnly,true))
  val replacement=mapOf(wifi to "wifi-2",cell to "cell-1")
  assertEquals(cell to "cell-1",VpnDnsPolicy.selectBondResolver(replacement,both,true))
  assertEquals(wifi to "wifi-2",VpnDnsPolicy.selectBondResolver(replacement,replacement,true))
  assertNull(VpnDnsPolicy.selectBondResolver(cellOnly,cellOnly,false))
  assertNull(VpnDnsPolicy.selectBondResolver(emptyMap<Int,String>(),both,true))
 }
 @Test fun singleRouterTrafficUpdatesExistingMetrics() {
  LabVpnService.txBytes=0;LabVpnService.rxBytes=0
  LabVpnService.record(JSONObject().put("event","profile_traffic").put("profile_id","a")
   .put("tx_bytes",31).put("rx_bytes",47).put("tcp_flows",2).put("udp_flows",3)
   .put("udp_tx",7).put("udp_rx",11).put("udp_rejected",13).put("datagram_drops",17))
  assertEquals(31L,LabVpnService.txBytes);assertEquals(47L,LabVpnService.rxBytes)
  assertTrue(LabVpnService.flowSummary.contains("2/3"))
  assertTrue(LabVpnService.flowSummary.contains("↑7 ↓11"))
  assertTrue(LabVpnService.flowSummary.contains("17"))
 }
}
