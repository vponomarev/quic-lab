# Xray/VLESS compatibility checkpoint — V0

Date: 2026-10-03. This is an integration checkpoint, not a VPN feature release.

## Selected dependency and build

- Xray release v26.3.27, module github.com/xtls/xray-core v1.260327.0, commit d2758a023cd7f4174a5a5fa4ff66e487d4342ba0; MPL-2.0. Selected from official releases API. Go requirement 1.26; tested toolchain 1.26.8.
- Xray source remains unmodified. Corresponding source and license texts are recorded in THIRD_PARTY_NOTICES.md and packaged Android assets.
- Xray's graph upgrades gVisor to January 2026, which breaks tun2socks v2.6.0's UDP forwarder signature. Explicitly retain gVisor May 2025. Narrow protobuf configuration excludes Xray WireGuard, TUN and arbitrary configuration loaders; enabling them requires a separate compatibility check.
- Xray uses apernet/quic-go, distinct from our patched quic-go module. There are two dependency paths; no replacement of our QUIC fork.
- Android arm64/amd64 AAR, debug APK and lint pass. APK before integration: 47,846,262 bytes; with Xray and new license assets: 82,509,408 bytes. These are comparable two-ABI debug APKs, not optimized release sizes.

## Socket and configuration boundary

- One immutable process-level SystemDialer routes by core.FromContext instance identity. Factories are registered per engine; unknown/closed identities fail closed. Creation is serialized because core.New changes upstream global DNS/outbound references.
- Do not use RegisterDialerController: upstream logs errors and continues. Do not use WithAdapter: it discards context.
- Disable Xray DNS strategies, dialerProxy, preconnect, reverse, native mux, sniffing and arbitrary inbounds. Build typed protobuf from the restricted Config, not a remote full Xray config.
- Android resolves each endpoint with its own Network using existing resolveEndpoint. The outer factory accepts only that resolved IPv4 endpoint; protect/bind must succeed before connect. A domain cannot fall through to Go's system resolver. Android platform DNS traffic is not attributed to the app's TCP meter; do not claim SIM billing-byte accuracy for it.
- Existing shared LTE meter accounts actual outer TCP bytes (including protocol/crypto overhead), closes sockets on exhaustion and blocks reconnects. IP/TCP headers and retransmission bytes are not measured by this userspace counter, consistent with existing transports.
- Close/unregister cancels in-flight factory calls, closes established and late connections. Per-flow cancellation also closes its outer socket, needed for REALITY failure cleanup.
- Xray's raw access/error logging is discarded because it can include destinations and UUIDs. Product diagnostics must use sanitized structured events.

## Server feasibility

A post-authentication dispatcher gate observes VLESS MemoryAccount UUID and the original inbound TLS/REALITY connection. Revocation atomically denies new admission, cancels active dispatch and closes that original connection. Do not wrap the concrete TLS/REALITY object: Vision relies on its type.

Linux tests prove actual TLS/VLESS traffic, rejection of a wrong UUID, closure of an active authenticated TCP flow and rejection of another flow with the retained credentials. A separate two-device test confirms revocation leaves the other device active. This does not yet implement admin persistence, enrollment, systemd packaging, UDP revocation or the demux-only destination policy; those are V2.

Upstream RemoveUser alone only changes authentication state; it does not close ordinary active connections. Upstream dynamic TLS certificate refresh triggered a reproducible race at BuildCertificates/GetCertificate; use OneTimeLoading and restart the owned server process when applying a certificate update. No claim of an upstream fix.

## Verification and remaining scope

- Full Linux Go suite and vet passed with the adopted dependency graph; targeted VLESS/mobile tests passed under race detection.
- Actual TLS round trip uses locally generated certificates, not real subscription secrets. Restricted REALITY/Vision protobuf validation passed.
- Android physical-network result is recorded below after execution; a built APK alone is not acceptance.
- UDP is explicitly rejected by this V0 client, without opening a socket. Standalone UDP requires a packet-capability implementation and tests before its V1 release. Demux framing can later carry user UDP over a TCP underlay independently of VLESS UDP support.
- core.Dial's net.Conn SetDeadline/SetReadDeadline/SetWriteDeadline are upstream no-ops. Current bounded HTTPS probe uses context cancellation/Close and explicit HTTP timeouts. A proper deadline adapter is required before exposing this as a general gateway flowDialer or implementing Echo through it.
- No Android profile import UI, VLESS TUN backend, managed server deployment, external demux path or subscriptions feature is claimed complete.
- Only one private fixed subscription profile is used for device testing. Full supplied Xray routing/balancer configurations are never executed; compatibility with the entire subscription is not claimed.

## Source references

- https://github.com/XTLS/Xray-core/tree/v26.3.27
- transport/internet/system_dialer.go and dialer.go: global socket/DNS boundaries.
- common/net/cnc/connection.go: deadline semantics.
- proxy/vless/inbound/inbound.go and app/proxyman/inbound/worker.go: authenticated identity and original connection.
- transport/internet/tls/config.go: certificate reload race path.
## Android execution result

Redmi Note 9 Pro, Android 11, USB dc69eb2c: VlessEngineTest passed (1 test, 2.259 s). Two simultaneously alive REALITY/Vision engines used separately resolved endpoints and protect/bind callbacks for Wi-Fi and LTE; both completed verified HTTPS requests to quic-demo.vpnc.ru. After disabling Wi-Fi, the LTE engine completed another request without recreation. Wi-Fi was re-enabled and the private fixture removed in finally. This is a transport test, not proof of preserving a TUN/game session.

The first run failed only because the expected HTTP code was 200; the site returns 302 at its root. A separate direct GET confirmed 302. The final test expects that exact fixture response, without following redirects. Provider credentials and endpoint are not retained in test output or Git.

V0 technical gate passed for TCP, TLS and the tested external REALITY/Vision profile. UDP, connection deadline adaptation, full import and production server integration remain explicit follow-up work. No release or production server update performed.
Android regression: VpnConfigurationTest, VpnExitLifecycleTest, TrafficBudgetTest and VpnBudgetRunTest passed (13 tests, 5.952 s) on the same installed Xray build. Final targeted Linux race passed (vless 1.207 s); vet passed. Final-response preservation test verifies a 917,504-byte TLS/VLESS response without truncation.

## V1 standalone implementation (2026-10-03)

The V0 statements above describe that historical checkpoint. V1 adds fixed profile import, a general gateway adapter and an Android standalone backend. Managed server/admin remains V2; external demux paths V3; WS/XHTTP V4; subscriptions V5.

- Import accepts a bounded (16 KiB) VLESS URI or exactly one VLESS outbound JSON object. TCP/RAW, TLS/REALITY and Vision use the restricted typed configuration. Full Xray configs, routing/balancers/chains, duplicate/unknown fields, unsafe TLS, private trust roots and unsupported transports are rejected, not silently simplified. URI security and SNI are explicit; fingerprint support remains V0's restricted set.
- Android supports paste, file and QR review/import. Canonical credentials are stored only in the existing encrypted identity file; portable settings contain endpoint/transport metadata. VLESS is a standalone exit, pool size one; maximum-availability/demux UI is disabled until V3. Standalone profile Echo is explicitly unavailable; VPN diagnostics remain accessible.
- The adapter obtains the genuine instance context through public common.RegisterConfig/core.CreateObject, then invokes Dispatcher.DispatchLink asynchronously. Two bounded 64 KiB queues implement deadline changes/reset, read/write backpressure, lifecycle cancellation and local CloseWrite. No copied private Xray context keys or upstream patch.
- Local CloseWrite seals upload but keeps replies readable. **Upstream VLESS does not transmit remote EOF at this point:** protocols that require the remote server to wait for EOF before answering are not supported. The initial real TLS EOF-delimited test exposed this; the retained test proves framed-response preservation, not transparent remote half-close.
- UDP keeps one packet per operation. Payloads over **8190 bytes** are rejected because the selected Xray ordinary VLESS encoder otherwise silently discards them. Inbound oversize replies are rejected before admitting fragments; the regression uses the actual upstream LengthPacketReader with a 9000-byte wire packet. Android VPN MTU is 1280. Xray Vision's default UDP/443 restriction is retained; universal UDP/QUIC compatibility is not claimed.
- Each physical-network change resolves the original endpoint on the new Android Network and recreates that profile's engine, retaining SNI. Old flows close; applications reconnect. This is not demux session continuity. Existing routing, fail-closed behavior, global LTE budget and independent exits remain in effect.
- Health is a certificate-verified HTTPS IPv4 response from api.ipify.org through the captured engine, at intervals of at least five seconds. It measures end-to-end HTTPS latency, not isolated outer-link RTT. Disabling RTT keeps rare health checks. New engine creation alone does not prove internet reachability. The probe depends on that external service; stale generation results are suppressed.

Verification so far: full Linux Go suite and vet passed; targeted race passed. Android build/lint passed and import/config/lifecycle/budget instrumentation passed (15 tests, 6.286 s). Physical standalone acceptance is recorded below after completion. An initial live-test failure was a fixture error: the client excludes its own UID, so acceptance uses the separate quicprobe app and asserts its active network is a VPN.
The standalone acceptance exposed a net.Conn lifetime mismatch: Android's TUN router cancels the dial context after DialContext returns. V1 now retains returned flows until Conn.Close or engine shutdown, as net.Dialer callers expect; a real TLS/UDP regression covers this. Internal per-flow cancellation still closes the tracked physical socket on shutdown.

A separate Vision regression feeds a 16 KiB application write through upstream ReshapeMultiBuffer. The original adapter lost 4096 bytes because upstream expects standard-sized buffers; TCP writes now use buf.Size (8192) chunks, and byte-for-byte preservation passes.

### V1 acceptance result

- Redmi Note 9 Pro / Android 11: **VlessVpnTest PASS**, 19.776 s. A separate captured app UID completed verified HTTPS and UDP DNS on Wi-Fi, LTE after Wi-Fi loss, and Wi-Fi after return. Each request confirmed Android's active network was VPN. The test uses a temporary imported REALITY/Vision profile; stop command, profile selection and Wi-Fi state are restored, fixtures removed.
- Final Android regression **16 tests PASS**, 8.313 s: simultaneous independent Wi-Fi/LTE engines plus import/configuration/lifecycle/budget tests. Together with the standalone test, 17 acceptance/regression tests passed on the final APK.
- Same single private external profile on Linux: **TestLiveFixedVLESSProfile PASS**, 0.611 s, verified HTTPS/IPv4 and UDP DNS. Private fixture removed afterwards. Subscription download was already established during V0; V1 tests connection use, not a subscription manager.
- Final full Linux suite PASS (mobile 53.550 s), full vet PASS, targeted race PASS after lifetime/Vision fixes (vless 1.308 s, mobile 1.067 s). Android assemble/lint PASS. APK 82,643,680 bytes (debug arm64/amd64).
- One fresh review identified inbound UDP fragmentation; its regression is fixed. Additional live-test and parent-review regressions (dial-context ownership and Vision buffer size) are fixed and covered. Source scan found no private fixture credentials.

V1 is an accepted local development checkpoint. No production server update, GitHub release or push was performed. V2 managed server is the next delivery; V3 demux continuity and subscriptions remain future scope. Existing phase-one all-apps Android limitation and outstanding long-run/load gates are unchanged.

## V2 managed-server acceptance (2026-10-03)

Managed Linux TLS and REALITY/Vision server, private worker/control/admission and Android managed profile integration are implemented. See [server behavior and acceptance](vless-server.md). Public isolated tests passed Android HTTPS/UDP on Wi-Fi, LTE and Wi-Fi return for both transports; stock Xray CLI TCP/UDP also passed. Five simultaneous connections per test UUID were revoked without dropping the sibling UUID; restart continued denying revoked credentials.

XUDP GlobalID is deliberately normalized to zero to prevent cross-device association reuse in the process-global upstream cache. No UDP association resumption across outer connections is promised. The full race gate requires disabling checkptr only in upstream Xray VLESS packages because Vision uses unsafe pointer arithmetic; project packages retain checkptr and the race detector remains enabled. This does not resolve the pre.5 external-profile LTE issue.

No512-device/100Mbps load result or repeated optical camera QR scan is claimed. Server binary delivery uses the existing installer; publication/deployment remains a separate checkpoint.
