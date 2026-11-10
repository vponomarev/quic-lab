# Linux ICMP Implementation Plan

Execute inline with superpowers:executing-plans and test-driven-development.
**Goal:** Real ICMP Echo and TTL diagnostics on the installed Linux client.
**Spec:** ../specs/2026-11-10-linux-icmp.md
**Architecture:** Linux LinkEndpoint interception, shared desktop ICMP exchange,
current VPN selection and bounded cancellation. Existing routes unchanged.

- [x] Add failing interception/reply/lifetime tests on Linux.
- [x] Implement Linux hook, shared exchange and current-profile cancellation.
- [x] Run race and isolated namespace Echo/TTL tests; independent code review.
- [x] Build, back up and update laptop; verify ping/traceroute -I, TCP/DNS/cleanup.
- [x] Update docs and acceptance report; commit. Leave QUIC enabled.

Review focus: no local fabricated success or direct fallback; malformed traffic
cannot enter the stack; current VPN after reconnect; cancellation without FD
reuse/write-after-close; preserve TCP/UDP path and ownership of borrowed TUN FD.
