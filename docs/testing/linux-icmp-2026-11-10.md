# Linux ICMP acceptance — 2026-11-10

Linux now intercepts IPv4 ICMP before gVisor and uses the same authenticated
QUIC/HTTPS or native AWG packet exchange as Windows. Original TTL, real reply
source/TTL and quoted request headers are preserved. Echo Reply, Time Exceeded
and Destination Unreachable are accepted only after correlation/validation.
ICMP follows the current VPN regardless of TCP/UDP application policy. Profile
replacement cancels outstanding probes; no direct fallback or synthetic reply.

The fdbased endpoint remains the sole TUN reader/writer and duplicate-FD owner.
The relay has 8 workers, a queue of 32 and a 3-second probe deadline. Traffic
counters include attempted ICMP messages and validated responses written to TUN.

## Automated checks

- Race tests: `internal/icmprelay`, `internal/gateway`, `internal/awg/...`,
  `internal/linuxclient`, `cmd/linux-vpn-client` and mobile
  `TestLinuxICMP|TestDesktop|TestAttachLink` passed on Linux.
- Regression coverage: unchanged original TTL, Echo/router errors, malformed
  packet drop, ordinary packet passthrough, no fabricated replies, reader detach, link close
  cancellation, pending probe cancellation during `ReplaceVPN`, selection of
  the replacement VPN, unavailable/unsupported VPN and no per-app/direct bypass.
- Windows shared code compiled on Linux with
  `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./mobile`; Windows runtime
  testing was not performed for this extraction of the existing exchange body.
- Namespace command:
  `VPN_PROBE_FULL=1 VPN_PROBE_ICMP=1 bash scripts/test-linux-client.sh`.
  QUIC/HTTPS/AWG Echo payloads 32/1200 bytes passed. QUIC/HTTPS TTL=1 received
  actual Time Exceeded from a third router namespace with the original quote.
  VLESS TCP/WS returned no fabricated/direct ICMP reply. TCP/UDP payload checks,
  partial route rollback, default/LAN preservation and stop cleanup were tested.
  Earlier runs exposed intermittent shutdown hangs after successful VLESS
  TCP/WS payload checks. A captured stack showed the fdbased reader still polling
  because the wrapper swallowed `Attach(nil)` after Close. A regression test
  reproduced this before the fix. Close now explicitly detaches/joins the reader
  before FD closure, and Attach/Close are serialized to prevent late reattachment.
  The complete five-transport matrix then passed, including all stop/route
  cleanup checks and unchanged host management routing.

## Installed laptop

Thinkpad-T14, Ubuntu 24.04.5 amd64, `vitaly.linux-laptop`:

- Installed binary SHA256:
  `f819af6016db5671525b4b83eb8b51f0c3159778108bdea96ec3a2b9ca02b150`.
- Backup of previous binary and user documentation:
  `/var/backups/quic-lab-client/20261110-042747-icmp`.
- QUIC: ping to 1.1.1.1, 56-byte payload 3/3 and 1200-byte payload 2/2;
  `traceroute -I -n -m 20 -q 1 -w 1 1.1.1.1` reached its destination at hop 14.
  First hops: 10.8.0.1, 172.19.0.1, 172.31.1.1.
- HTTPS: ping 2/2, first three ICMP traceroute hops returned.
- AWG: ping 2/2; first hops 10.77.0.1, 10.8.0.1, 172.19.0.1.
- VLESS: expected ICMP timeout, HTTPS traffic successful.
- Final build repeated 1200-byte ping 2/2 and first three trace hops on
  QUIC/HTTPS/AWG, plus VLESS TCP and expected ICMP timeout. Observed service
  stops took 0.26–0.27 seconds with route/DNS/IPv6 restoration.
- All four transports returned public exit 168.119.242.240 over HTTPS.
- Stop restored the original default/LAN routes, removed protocol-242 routes
  and temporary DNS override, and restored IPv6. Restart restored VPN DNS;
  DNS resolution and ping succeeded. LAN still uses wlp3s0 directly.
- Final state: QUIC active and enabled at boot, two `/1` capture routes and
  physical server pin. Profiles, helper and production server settings unchanged.
- User instructions updated at `/home/vitaly/QUIC-Lab/README.md` and
  `/usr/local/share/quic-lab/README.md`; installation status appended alongside.

## Boundaries

Use `sudo traceroute -I -n 1.1.1.1`. UDP/TCP traceroute TTL forwarding is not
implemented. VLESS, IPv6 ICMP, fragments and oversized Echo remain unsupported.
ICMP traces show the path from the VPN exit, not encrypted transport hops or the
UDP relay. Routers may suppress replies, so individual `*` hops are expected.
No production server changes or laptop OS/package upgrade were required.
