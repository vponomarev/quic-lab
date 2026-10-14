# IPv4 ICMP Echo and TTL over Windows QUIC/HTTPS

The Wintun reader intercepts IPv4 ICMP before the TCP/UDP stack. Valid, unfragmented Echo requests fitting the 1280-byte TUN MTU (up to 1252 data bytes without IP options) use an authenticated `icmp-echo-v2` stream. The server checks the existing destination allowlist and sends a real Echo request through its configured exit. With Linux transit, the raw socket binds to the same transit source IP as TCP/UDP. There is no fabricated local reply or direct egress fallback.

V2 carries the original IPv4 packet. An unconnected raw socket sets its outgoing TTL from the request and receives Echo Reply, Time Exceeded and Destination Unreachable from the destination or intermediate routers. The response uses the actual peer address and received TTL. Outgoing identifiers are randomized; replies are matched by checksum, identifier, sequence and payload (or the quoted request header). Error quotations are restored to the original client's IP header and ICMP identifier. Next-hop MTU is preserved; extra quoted data/extensions are not forwarded.

Traceroute describes the path **from the server's exit**, not the physical hops hidden inside QUIC/HTTPS or the AWG uplink. TTL 1 therefore identifies the first router after that exit. Silent/rate-limited routers still produce timeouts. This is an Echo diagnostic extension, not a general raw IP tunnel.

Limits: 8 client workers, a 32-packet queue, 32 server probes and a three-second deadline per probe. Overflow drops diagnostic packets without blocking TCP/UDP. Stop cancels outstanding probes. IPv6, fragmented/oversized Echo and other ICMP types are unsupported. Client TX/RX now includes ICMP messages (including the ICMP header, excluding outer IPv4/tunnel overhead); TX records attempted probes and RX records validated replies delivered to TUN. Counters refresh through the existing traffic event and reset for a new connection.

## Compatibility and deployment

The Linux server process requires CAP_NET_RAW. The production service already has it for packet capture; no privileges were added. Stock example units do not grant it. An administrator opting into Echo can install deploy/icmp-echo.conf as a systemd drop-in; if a custom CapabilityBoundingSet is in effect, CAP_NET_RAW must also be included while preserving existing capabilities. Do not enable capture or grant CAP_NET_ADMIN solely for Echo.

New servers retain `icmp-echo-v1` for old Windows clients. A new client falls back to v1 only after explicit `unsupported network` rejection and request TTL >=64 (conventional ping). V1 cannot preserve TTL and reconstructs the reply TTL as 64. Low-TTL probes never fall back, so old servers cannot produce a misleading one-hop trace. Authentication failures, timeouts and unsupported custom exits do not trigger fallback. Servers predating ICMP entirely cannot answer Echo.

TCP/UDP protocol and Android builds are unchanged; the ICMP TUN hook is Windows-only. Android does not gain this TTL extension automatically.

## Verification — 2026-10-14

- Race tests cover validation, router error quotation, checksums, wrong peers, legacy negotiation, destination ACL, custom exit rejection, worker cancellation and ICMP traffic counters.
- Live Linux probe: TTL 1 returned Time Exceeded from 192.168.5.1; TTL 128 returned Echo Reply from 1.1.1.0 with received TTL 57.
- Installed Windows service over QUIC: 32-byte and 1200-byte Echo payloads matched (72/74 ms). TTL 1–4 returned Time Exceeded from 10.8.0.1, 172.19.0.1, 172.31.1.1 and 49.12.25.119; TTL 5–7 timed out; TTL 8 returned 213.239.227.214.
- Before any TCP/UDP application checks, ICMP counters read TX=1568, RX=1428. HTTPS exit verification, UDP/TCP DNS, GUI close/reopen, Stop and cancellation also passed.
- Production updated to the v2 server and the installed Windows Preview service/engine updated. The original published preview archive remains unchanged.
- Live Windows HTTPS: Echo 32/1200 bytes passed (85/77 ms); TTL 1–4 returned the same router addresses. Native `tracert -d -h 8 -w 700 1.1.1.0` displayed these hops and 213.239.227.214 at hop 8, with expected silent hops. The bounded engine stopped and cleanup completed. This bounded test did not attempt to reach the destination within eight hops.
