# Unified ingress implementation plan

Spec: ../specs/2026-10-03-unified-ingress-design.md
Base: 14f666e. Worktree: phase1-vpn; branch: codex/phase1-vpn.

- [x] 1. Add typed SNI routes, strict validation and PROXY v2 forwarding. First add failing routing/source tests; implement; run cmd/server tests on Linux. Contract: server JSON tls_routes array with server_names (exact strings), target (host:port), proxy_protocol (boolean). PROXY routes require numeric loopback targets. Existing tls_fallback remains supported.
- [x] 2. Add opt-in accept_proxy_protocol to managed VLESS config and Xray socket settings. Reject non-loopback listeners. Verify TLS and REALITY authentication/revocation and original source metadata; expose bounded source information in admin diagnostics.
- [x] 3. Share QUIC listener by ALPN while requiring mTLS for VPN/bond. Introduce a small Accept interface for echo/gateway, test auth separation and shutdown, integrate startup and retain split ports.
- [x] 4. Update installer: unified fresh defaults, explicit upgrade migration, private VLESS backend and SNI route generation, configurable split ports, ACME SAN issuance/renewal and manual certificates. Extend installer tests before implementation; retain recovery/admission safety.
- [x] 5. Update operator docs, exported profiles, version and release notes. Verify all Go tests/vet, focused races, installer suite, Android build/lint and available phone integration. Review whole change and resolve important findings.
- [x] 6. Build final assets, deploy with backups and rollback instructions, issue certificate for both names, verify all live protocols, publish APK, install test phone, push Git/tag and publish GitHub release. Verify public downloads and record final status.

Tests and test binaries execute on root@192.168.5.214, never on Windows. Production SSH uses llm-user with sudo. No credentials in logs, documentation or commits. This plan does not add subscriptions or VLESS transit multiplexing; those retain their roadmap positions.


2026-10-03: shared HTTPS preserves optional `public_tls_min: 1.0` for Web UI; both VPN HTTP routes require TLS 1.3 and verified client certificates. Production migration and ordinary reinstall succeeded after fixing DynamicUser StateDirectory and credential ACL compatibility. APK pre.6 is deployed and installed on the test phone. Public TLS 1.0 UI, UDP/443 echo, final Go test/vet, focused races and installer suite passed. Authenticated live acceptance passed: public QUIC/HTTPS mTLS payload, VLESS TCP/UDP and source IP:port, revocation, Android Wi-Fi/LTE/Wi-Fi. Temporary credentials and account removed. ACME renewal dry-run passed. Release v0.8.0-pre.6 published: https://github.com/vponomarev/quic-lab/releases/tag/v0.8.0-pre.6 . GitHub asset digests match final local archives and public APK.
