# Linux full IPv4 routing acceptance — 2026-11-10

Implemented explicit `targets: ["0.0.0.0/0"]` with two /1 TUN routes,
preserving the existing physical default and more-specific routes. If needed,
a physical /32 exception pins the VPN endpoint before capture is installed.
Normal stop and partial startup failures remove only the owned exception.

## Automated evidence

- RED: TestFullIPv4Configuration rejected /0, and
  TestFullIPv4PreservesSpecificRoutes rejected connected LAN routes.
- GREEN: `go test -race ./internal/linuxclient ./cmd/linux-vpn-client -count=1`.
- Full namespace matrix: QUIC, HTTPS, AWG, VLESS TCP/TLS and WebSocket/TLS.
  TCP/UDP payloads, unchanged physical default, LAN bypass, endpoint via gateway,
  exactly two capture routes, graceful cleanup, and injected second-/1 failure
  rollback all passed. QUIC/HTTPS server-loss cleanup passed as well.
- Per-app full-capture matrix: both selection modes, IPv4/dual-stack sockets,
  VPN/direct source identity, captured DNS, unknown-owner policies and cleanup.
  Physical default has metric zero in this fixture.
- Independent review found an onlink gateway regression. A failing unit test
  reproduced it; the pin now preserves reachability of the kernel-resolved
  physical nexthop. A /32-underlay/onlink/metric-zero namespace passed payloads,
  partial-start rollback and server-loss cleanup.
- Tests ran only on the authorized Linux source-only host. No user keys or
  profiles were copied there. Host management routes remained unchanged.

## Installed laptop

ThinkPad T14, Ubuntu 24.04.5. Binary SHA256:
`b7fc2e82a47ad8af0969b0e32de6e141e3b555c7b49c5214fb52940e80f0fa41`.
Eight full/Firefox profiles migrated; subnet examples retained.
Four real transports passed HTTPS exit-IP verification, UDP DNS, system DNS,
LAN/Docker/endpoint routing, exact two-route capture and stop cleanup.
The 8.8.8.8/32 example passed independently. QUIC was left active and enabled.
Server AWG transit via the relay was not changed in this development iteration.

The laptop-specific DNS helper hit systemd-resolved's restart burst limit during
rapid transport switches. Logs confirmed start-limit-hit. Explicit resolver
restarts now reset its failed/rate-limit state first; the repeated complete
laptop matrix passed, including resolver restoration after each stop.

Rollback files stay root-only under
`/var/backups/quic-lab-client/20261110-033748` on the laptop.
User instructions and current status are in `/home/vitaly/QUIC-Lab/`.

## Limits

No automatic underlay migration or kill switch. Client itself does not configure
DNS or IPv6; the installed laptop service retains reversible DNS/IPv6 setup.
SIGKILL deletes the TUN and /1 routes but can leave the physical endpoint pin;
normal systemctl stop cleans it. Avoid blind cleanup by endpoint/protocol alone:
a different running client may own an otherwise matching route. Reboot and
Firefox GUI operation were not tested; the per-app engine was namespace-tested.
