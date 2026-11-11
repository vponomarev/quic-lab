# Linux GUI live health and RTT implementation plan

> Execute inline using superpowers:executing-plans. User authorized autonomous development and laptop updates.

**Goal:** Stable controls during status polling, transport health and fresh RTT using the same probes as Android.

**Design:** Reuse mobile.Gateway diagnostics: QUIC/HTTPS echo, AWG tunneled ICMP, VLESS verified HTTPS. Linux explicitly uses a 1 s probe cadence (VLESS minimum 5 s). Forward only whitelisted numeric/status fields from the CLI to its journal. Controller reads the newest diagnostic in the current systemd invocation separately from readiness transitions. Freshness uses the journal's monotonic timestamp and Android's max(3 × interval, 5 s) rule. GUI shows transport response and DNS readiness separately; missing, failed, disconnected or stale probes never show a current RTT. Closing the GUI leaves VPN running.

**Constraints:** No new external probe endpoints, profile changes, server changes or direct fallback. Preserve routing/ICMP behavior. No raw transport errors, addresses, keys or connection IDs exposed by diagnostics. Scope old gateway callbacks to their gateway lifetime when recovering.

**Stack:** Existing Go Linux client/mobile.Gateway, Python control socket, GTK 3.

- [x] Reproduce poll flicker and dropped clicks; serialize pending actions without disabling controls for read-only polls. 22 existing/new Python tests pass; independent review clean.
- [x] Add failing tests for sanitized Go diagnostic events, retired gateway callbacks, current-session journal samples, freshness, failed probes and GUI RTT display.
- [x] Implement diagnostics forwarding/cadence, controller extraction and GTK status/RTT. Run focused Go race tests and Python/GTK suite on the authorized source-only Linux host.
- [x] Review changes; build Linux client; back up and update laptop client/GUI. Check all four existing transports, RTT freshness and control stability, restore QUIC active/autostart.
- [x] Update usage/acceptance docs and local setup package, commit verified changes.

Acceptance: 27 Python/GTK/systemd tests, focused Go race tests and five-transport
namespace matrix passed. Independent review clean after the pending-command
RTT regression was fixed. Laptop: all four profiles show fresh RTT and the
expected exit; six GUI polls produced no disabled-control transitions. Closed
verification window preserved VPN InvocationID. Final QUIC active/autostart,
LAN direct and public traffic through TUN. Usage docs and setup archive updated.
See docs/testing/linux-gui-2026-11-11.md for evidence and remaining platform QA.
