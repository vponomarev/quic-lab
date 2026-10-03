# Unified ingress design — 2026-10-03

Status: authorized by the user; implementation pending.

## Requirements

Default installation exposes TCP/443 for HTTPS/mTLS, Web UI and VLESS selected by exact SNI, and UDP/443 for QUIC VPN and echo selected by ALPN. Split ports remain configurable. AmneziaWG remains on its own configurable UDP port (explicitly agreed). Existing manual configuration and certificates remain supported. Certificate acquisition and renewal use ACME with unattended operation. User authorizes release, Git publication, deployment to quic-demo.vpnc.ru, APK publication and test-phone update after verification.

Production names: quic-demo.vpnc.ru for existing local HTTPS/mTLS and ui.quic-demo.vpnc.ru for VLESS. Both A records resolve to 87.228.55.179 (checked 2026-10-03). The ui prefix does not change the agreed purpose of the second name: VLESS routing.

## Boundaries

The Go server owns the public socket. It inspects and replays ClientHello without terminating backend TLS. Local SNI names go to existing local HTTPS/mTLS handling; configured exact SNI routes go to private TLS backends. The legacy nginx fallback remains available. SNI collisions with local names, duplicate routes, invalid targets and self-routing are rejected. Unknown names without fallback are closed. Routing has bounded ClientHello size, handshake deadline and concurrency.

VLESS receives PROXY protocol v2 before TLS and preserves the public client source IP and port. Accepting PROXY headers is opt-in and requires a numeric loopback listener. Public clients cannot supply trusted PROXY headers through the router. TLS/REALITY authentication and device admission remain unchanged; source metadata is diagnostic and never an authorization identity. Preserve Xray's original authenticated connection type for Vision.

Shared QUIC dispatches by negotiated ALPN. VPN and bond ALPNs require client certificates; echo does not. Ambiguous offers must not bypass VPN authentication. Existing separate-listener configuration remains supported. Shared listener shutdown must release both accept loops and active connections.

Installation defaults to unified ports on fresh installs. Existing configurations are preserved unless migration is explicitly selected. Migration updates exported profiles; older profiles with old ports require refresh. Do not touch unrelated Xray/3x-ui or unmanaged nginx services. ACME obtains a SAN certificate for both configured TLS hostnames, validates DNS/challenges, and installs a renewal hook for affected services. Existing manual cert/key paths and manual route configuration remain possible.

## Acceptance

Tests prove exact SNI routing, TLS byte replay, source address forwarding, invalid route rejection, private-only PROXY trust, VPN mTLS isolation on shared UDP and separate-port compatibility. Linux runs server tests, race checks and installation tests. Android builds and ADB run on Windows. Public verification covers HTTPS/Web UI, mTLS VPN, QUIC VPN, echo, VLESS TCP/UDP and observed source metadata. Release APK hashes must match GitHub and the public download. Capacity must not decrease below the already supported maximum; minimum acceptable maximum is 30 devices.
