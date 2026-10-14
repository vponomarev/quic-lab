# Windows full IPv4 and uplink recovery implementation plan

Approved in chat: full IPv4 default for new settings, selected subnets retained, automatic external interface selection/reconnect, IPv6 completely untouched, LAN reachable, no kill switch/session preservation promise. Execute inline using executing-plans and TDD; existing isolated branch.

1. Add routing_mode=all_ipv4/subnets (missing=legacy subnets), uplink_mode=auto/manual (missing=manual), dns IPv4. Pure prefix subtraction produces complete IPv4 coverage minus reserved/on-link/endpoint addresses, without modifying physical routes. Validate AWG coverage. Unit tests first.
2. Windows discovery chooses physical IPv4 default by combined metric; manual mode retains selected interface. Resolve server outside owned routes. Configure only owned IPv4 routes; DNS policy uses scoped owned rule with process identity for cleanup. Never touch IPv6 adapters/routes/bindings. Test route and cleanup ownership.
3. Auto backend watches IPv4 interface/address/route changes, debounces and retries; serial stop-cleanup-select-start. Preserve persisted hostname/settings. Explicit runtime waiting/reconnecting state; Stop cancels retry and prevents resurrection. Fake lifecycle/race tests.
4. WPF mode/automatic-interface controls and truthful status. Preserve old settings and downloaded local routing. UI acceptance.
5. Native bounded full-mode test with external watchdog, IPv4 route/DNS restoration and unchanged IPv6 snapshot; reconnect simulation, regression, independent review, installed build and evidence. Do not claim physical LTE tested without modem access.

Ruling: prefix subtraction instead of a mutable /32 physical bypass route. Endpoint still follows the chosen physical socket binding; all installed prefixes belong to our disposable adapter. Cost: more routes, but safe ownership/cleanup across gateway changes.

Ruling: preserve existing non-default routes on other active adapters (including VirtualBox, Hyper-V and ZeroTier), not only hardware on-link LANs. Otherwise the current host cannot enter full mode without breaking its existing explicit network routes. Full mode covers remaining IPv4; UI explains these exclusions. Cost: explicit system routes remain exceptions to full mode.

## Completion ledger

- Model/prefix algebra and legacy mode migration: tests RED to GREEN.
- Auto backend: startup lifetime cancellation regression RED to GREEN; loss/recovery, same-index gateway, cleanup fence and Stop tests pass under race detector.
- WPF mode editor RED to GREEN; dashboard and URL-import regression pass.
- Final review fixed unchanged-network dead transport recovery (regression RED to GREEN), large encoded command overflow (native 70,000-character RED to GREEN), CLI stale DNS before failed/subnet startup (native test passed).
- Native testing additionally reproduced pending TCP handshake shutdown blocked in gVisor Stack.Wait. Input now closes/waits before stack shutdown; shutdown order RED to GREEN and three successive real full-mode QUIC cycles passed. HTTPS and failed-start DNS recovery passed. IPv6 route snapshot unchanged, own adapter/DNS removed, previous VPN restored.
- Existing contract-test project omitted SettingsDraft.cs after the earlier editor changes; added its compile link after observing compiler failure. Contract and traffic checks passed.
- Installed engine/service/UI hash verification and original encrypted profile hash unchanged; restored VPN ping passed. Physical LTE/Wi-Fi transition and sleep remain hardware acceptance limits, not claimed tested.
