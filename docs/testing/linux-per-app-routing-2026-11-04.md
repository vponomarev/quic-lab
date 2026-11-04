# Linux per-app routing — 2026-11-04

Base: `d69276d`, worktree `phase1-vpn`, branch `codex/windows-client-beta`.
Source-only testing on the authorized Linux host in `/tmp/quic-android-stability-20261104`.
No real profiles or host routes were activated. Unrelated admin/cell/MDM changes are excluded.

## Implemented

- Version 2 policy: `only_selected` / `all_except_selected`, independent lists,
  case-sensitive canonical executable paths, unknown `tunnel` / `direct` / `block`.
- Procfs TCP full tuple and UDP exact/wildcard/connected tuple attribution,
  combining IPv4 and IPv4-mapped/dual-stack candidates from both socket-table families;
  complete visible-process scan, shared ownership, anchored PID directories,
  start-time/executable/FD rechecks and second socket-table snapshot.
- Four process-wide calls with 250 ms caller timeout; bounded tables/processes/FDs/owners.
  Incomplete scans never publish the subset as a resolved owner.
- Shared desktop packet router, protected direct sockets, borrowed TUN FD duplication,
  cleanup, aggregate counters and pre-network configuration validation.
- CLI JSON configuration and optional systemd drop-in for cross-user visibility.

## Evidence

- `go test ./... -count=1 -timeout=180s`: 38 packages PASS on the isolated source tree.
- `go test -race ./internal/approuting ./internal/linuxclient ./cmd/linux-vpn-client ./mobile -run 'Test(Proc|Linux|Desktop|TUN|Bounded|AppRouting)' -count=1 -timeout=90s`: PASS.
- Additional live cross-UID attribution (child UID/GID 65534) and proc tests under race: PASS.
- `go vet ./internal/approuting ./internal/linuxclient ./cmd/linux-vpn-client ./mobile`: PASS.
- Windows approuting tests cross-built on Linux and run natively: 16 PASS.
  Linux path keys do not change Windows case folding. No new live Windows TUN acceptance.
- `systemd-analyze verify` on a temporary unit plus per-app drop-in: PASS. This is syntax
  verification with the built executable, not installation/service lifecycle acceptance.
- `bash scripts/test-linux-per-app.sh`: 25 scenario groups PASS: five transports ×
  two modes plus three unknown policies, with TCP and UDP in every group.
  Each known-mode group also verifies IPv4 through AF_INET6 TCP/UDP sockets,
  captured UDP DNS, aggregate counters and cleanup.
  Host management route unchanged; namespace TUN/routes removed after each run.
- `bash scripts/test-linux-client.sh`: legacy mode without app routing PASS for all five
  transports, including startup-deadline boundary, TUN cleanup and QUIC/HTTPS server-loss cleanup.

The two probe processes execute different binary files and reach **the same destination**,
`192.0.2.2`. The receiver returns the actual peer address and payload. Direct is
`198.18.0.1`; QUIC/HTTPS/VLESS TCP/VLESS WS VPN is `192.0.2.2`; AWG VPN is `10.55.0.2`
(the real inner source in AWG's test netstack). An unlinked running probe exercises
unknown ownership: tunnel/direct return the expected source, block returns no payload
and increments blocked counters. This does not rely on echo success alone.

Initial AWG source-echo run failed because the pre-existing fixture listened only on
9001/9002 inside its netstack. Added opt-in source-echo endpoints there; the full matrix
then passed. No transport implementation change was needed.

Local artifacts: `artifacts/linux-per-app-20261104/` contains Linux binary,
Windows compatibility test binary and test logs. Artifacts are not committed.
Final Linux executable: `quic-lab-client`, SHA-256
`9e282ae21d20301b1c69acfa0412aca92ec2f9843f2211b85bbd09de1704c47f`.
All 17 scoped source/fixture/template files match the test host byte-for-byte
(`source-sha256.txt`). The final 38-package suite, targeted race/vet and expanded
25-group netns matrix passed after the dual-stack fix.

## Limits and remaining acceptance

Root and complete proc/file visibility are required for useful attribution across users.
Containers, distinct PID/mount namespace views, LSM restrictions and nonstandard thread
FD ownership are not part of physical acceptance. Procfs snapshots cannot make ownership
atomic or a kernel security boundary. Deleted executables use unknown; scripts are matched
by their interpreter. First-flow scan cost and timeout frequency need desktop/load measurements.

Capture remains configured IPv4 subnets. Captured port 53 stays on VPN; system DNS and IPv6
are unchanged. Restart applies rules and terminates direct flows too. No GUI, kill switch,
full IPv4 capture, hot updates or independent reconnect is implied. Physical applications,
REALITY, installed systemd lifecycle, sleep/network changes remain unverified here.
The pre-existing REALITY/checkptr limitation from the transport-parity report still applies;
the targeted race command above is deliberately not a claim of a full race-clean suite.

## Decisions

1. Bounded procfs attribution instead of new kernel/eBPF integration: avoids new dependencies
   and privileged filter lifetime, at the cost of first-flow scans and conservative unknowns.
2. Preserve subnet capture and restart-to-apply: compatible with the existing Linux CLI;
   all-IPv4 capture, GUI and hot policy replacement remain separate work.
3. Atomic/adversarial ownership remains outside the userspace guarantee: bounded snapshots
   are best effort; hostile socket transfer/reuse can still defeat attribution.
4. Containers/distinct PID or mount views remain unsupported: visible procfs cannot prove
   invisible ownership; unknown or omitted co-owners are possible there.
5. Unshared thread FD tables/exited group leader remain unverified and unsupported in this
   process-directory preview; unusual thread ownership may be missed.
6. Desktop performance is further acceptance: tested bounds do not establish workload
   latency/CPU; large systems may fall back to unknown more frequently.
7. Installed systemd/sleep/network changes remain further acceptance: template syntax and
   namespace cleanup do not establish unattended service recovery.
8. REALITY/providers/physical apps remain further acceptance: local fixture success may not
   cover provider- or application-specific behavior.
9. Full IPv4 capture, IPv6 routing, managed DNS, kill switch, GUI, hot updates and independent
   reconnect remain outside this slice; users needing them must await later work.
10. Unrelated dirty work and pre-existing transport issues remain excluded; this commit
    does not certify those changes.

Implementation references: [procfs identity and access](https://www.kernel.org/doc/html/latest/filesystems/proc.html),
[IPv4 proc socket tables](https://www.kernel.org/doc/html/latest/networking/proc_net_tcp.html),
[systemd sandbox settings](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html).

## Final review

One fresh-context read-only review, gpt-6-astra/high. No Critical findings. One Important
finding fixed in one pass: IPv4 originating from AF_INET6 sockets was absent from ownership
candidates, potentially producing unknown or incorrectly resolving a different UDP owner.
`TestProcIPv4MappedSocketOwnersAreNotOmitted` and
`TestLinuxOwnerLiveIPv4OnDualStackSockets` were observed RED before the fix, GREEN after it.
Connected/wildcard mapped UDP and inaccessible second-table regressions are also covered.
The netns fixture now forces AF_INET6 sockets for both executable probes in both list modes.

Deferred Minor: deterministic tests for FD/inode changes mid-lookup, executable/start-time
changes between checks, restricted proc detection, mismatched executable filesystem identity,
and the process-count limit. The checks exist; existing race/fixture tests do not claim to
exercise every one of these branches. Broader hardening coverage remains follow-up work.
All behaviors the reviewer declined to certify are explicitly decided in items 3–10 above.
