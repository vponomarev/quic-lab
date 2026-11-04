# Linux standalone VPN readiness

Autonomous continuation from 57fedae. Add opt-in health_dns IPv4 to Linux AWG/VLESS
profiles. It must lie within targets (therefore AWG AllowedIPs too). The new DNS
health mechanism adds no implicit public target, system resolver or direct fallback.
Existing profiles and transport diagnostics remain unchanged: VLESS still runs
its HTTPS exit-IP heartbeat to api.ipify.org:443 independently of DNS readiness.
Chosen DNS roundtrip instead of custom echo daemon or public HTTP dependency:
ordinary user-supplied DNS is usable with third-party providers, but only proves
UDP/DNS reachability through that VPN, not all destinations/TCP or expected exit IP.

Shared desktop-only Gateway.ProbeDesktopDNS(ctx, IPv4): random ID/question under
.invalid, UDP port 53 through datagram backend, strict reply/question/opcode and
success/NXDOMAIN validation, bounded read/write/dial, cancellation closes socket.
No Android API changes. Reject direct-only/stopped gateways.

Linux single DNS health worker per gateway: checking -> vpn_dns_ready on success; degraded
after errors, unavailable after 3 consecutive errors; probes every 5 seconds,
each <=2 seconds. Unavailable is sticky until recovery. Stop cancels and joins.
Configured health enables bounded existing recovery even without per-app. Recovery
retires VPN flows, keeps TUN/direct, and requires successful DNS probe before
publishing the new VPN generation. Failed attempts retain existing backoff.
Separate readiness events; engine_ready without opt-in does not promise remote health.
SIGHUP still accepts app-policy changes only. Physical network switching separate.

Verify unit integrity, cancellation/single worker/stale generation; config bounds;
namespace AWG/VLESS TLS TCP+WS server loss/recovery, same direct TCP/UDP and TUN;
Stop during failure; legacy regression, full Go/race/vet, cross Windows build;
one fresh final review, scoped local commit and artifacts. No host routes/installed
profiles changed. DNS service failure can trigger reconnect even when VPN is fine;
document this tradeoff, no claim that all AWG/VLESS remote readiness is solved.
