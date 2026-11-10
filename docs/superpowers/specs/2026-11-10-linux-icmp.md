# Linux ICMP Echo and TTL

User requests Windows-equivalent ICMP support and installation on their Linux
laptop; autonomous execution remains authorized. Preserve current /1 routing,
DNS/IPv6 service setup and production AWG relay configuration.

Reuse icmprelay and the authenticated icmp-echo-v2 exchange for QUIC/HTTPS;
AWG uses its existing packet-level EchoPacket. VLESS stays unsupported, with no
direct fallback or synthetic replies. IPv4 Echo and related Time Exceeded /
Destination Unreachable only; UDP/TCP traceroute, IPv6 and fragmented Echo are
outside this iteration. ICMP captured by TUN always uses VPN, as on Windows;
per-app attribution remains TCP/UDP only.

Intercept at the Linux LinkEndpoint dispatcher before the TCP/UDP stack rather
than adding another TUN reader. This preserves existing fdbased lifetime and
packet I/O. Send validated replies through the same link. Reuse bounded relay
workers/deadlines; cancel on stop and VPN replacement, select the current VPN
per probe. Count ICMP bytes in VPN traffic totals.

Tests: link interception (TTL/replies/errors, malformed and unsupported packets,
non-ICMP passthrough, closure), shared protocol/AWG race suites, real namespaces
with Echo and TTL router errors; laptop ping and traceroute -I via QUIC/HTTPS/AWG,
VLESS explicit lack of support, TCP/DNS and start/stop regression. Leave QUIC
active/enabled; retain a root-only rollback and update user instructions.
