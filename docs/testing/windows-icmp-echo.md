# IPv4 ICMP Echo over Windows QUIC/HTTPS

The Wintun reader intercepts IPv4 ICMP before the TCP/UDP stack. Valid, unfragmented Echo requests fitting the 1280-byte TUN MTU (up to 1252 data bytes) use an authenticated `icmp-echo-v1` stream. The server checks the existing destination allowlist and sends a real Echo request through its configured exit. With Linux transit, the raw socket binds to the same transit source IP as TCP/UDP. There is no local fake reply or direct fallback.

Reply type, code, checksum, identifier, sequence and payload are matched. Linux raw-socket IPv4 headers are validated and removed. The server uses a randomized outgoing identifier and restores the caller's identifier in the reply. The client reconstructs an IPv4 header with TTL 64: this TTL is not a measurement of the remote path.

Limits: 8 client workers, a 32-packet queue, 32 server probes and a three-second deadline per probe. Overflow drops diagnostic packets without blocking TCP/UDP. Stop cancels outstanding probes. IPv6, fragmented/oversized Echo, ICMP error forwarding and traceroute are not implemented by this extension. Existing payload TX/RX counters still describe TCP/UDP and do not include Echo traffic.

## Server deployment

The Linux server process requires CAP_NET_RAW. The existing production service already has it for packet capture; no privileges were added for this deployment. Stock example units do not grant it. An administrator opting into Echo can install deploy/icmp-echo.conf as a systemd drop-in; if a custom CapabilityBoundingSet is in effect, CAP_NET_RAW must also be included while preserving existing capabilities. Do not enable capture or grant CAP_NET_ADMIN solely for Echo.

Old servers reject the new request kind. TCP/UDP protocol and Android builds are unchanged; the ICMP TUN hook is Windows-only.

## Verification

- Race tests: packet validation, reply integrity, no fabricated reply after failure, worker cancellation, server destination ACL and rejection of mismatched responses.
- Regression reproduces Linux raw replies with an IPv4 header (the first real test exposed this difference from a mocked socket).
- Live Windows service/QUIC: 1.1.1.0, data sizes 32 and 1200, validated response payload; HTTPS exit, DNS UDP/TCP, Stop and cancellation passed.
- Captured request and actual reply on production ql-exit0; no claim of full raw IP VPN support.
- Live Windows HTTPS: 32-byte and 1200-byte Echo payloads matched, 88/91 ms; bounded foreground engine stopped and cleaned up.
