# Linux per-app routing

User requests the same mode as Windows. Reuse the desktop TUN/direct router and
version 2 policy schema, with case-sensitive absolute Linux executable paths.
The two remembered lists and unknown VPN/default, direct, block semantics match
Windows. A nil/disabled policy preserves existing Linux forwarding. Capture scope
remains the configured IPv4 subnets; DNS port 53 in that scope stays on VPN;
system DNS and IPv6 are unchanged. No host-wide default route or GUI is added.

Owner resolution reads IPv4 and IPv6 socket tables from the client's network namespace
(including IPv4-mapped and wildcard dual-stack candidates for captured IPv4),
matches TCP full tuple and UDP wildcard/exact local plus connected remote endpoint,
then finds every visible process holding candidate socket inodes. Never select the
first PID or infer an application from UID/name/command line. Recheck socket inode
mapping and process start time/executable identity; no cached PID decisions.
Anchor process reads to open proc directories; PID reuse cannot redirect a lookup.
Resolve /proc/PID/exe against the same file in the client's filesystem; deleted
executables or conflicting mount views are unknown. Interpreter-based programs
are identified by their interpreter executable, not the script or launch command.

Require visible procfs for attribution. Restricted/incomplete scans, inaccessible
FDs, unsupported address families, missing/shared/mixed owners are explicit unknown
or shared qualities; never accept partial results as a known sole owner. Bound
native lookup across client generations to four calls/250 ms caller timeout,
4 MiB per socket table, 4096 processes, 65536 inspected FDs and 16 candidate owners.
No kernel isolation guarantee: userspace attribution is best effort and not a
security boundary. Existing flow decisions remain pinned.

Canonicalize active policy paths before TUN creation. Disabled stale lists stay
editable. CLI -validate checks active paths without network changes and displays
capture/unknown/DNS policy. Runtime JSON emits aggregate per-app counters, never
paths/PIDs. Changing config requires restart; stopped or failed VPN retains normal
existing cleanup behavior. Independent VPN/direct reconnect is later work.

Acceptance: policy fixtures and proc bounds/reuse/partial-result tests, real local
TCP/UDP attribution, and two different executable processes reaching the same
destination through distinct VPN/direct source addresses in private network
namespaces. Cover both modes, unknown handling, TCP/UDP, cleanup and the existing
transport matrix. No user's real profile/network is activated; use the already
authorized source-only Linux test host.
