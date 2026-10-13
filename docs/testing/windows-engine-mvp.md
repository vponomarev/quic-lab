# Windows engine MVP — 2026-10-14

Working foreground engine checkpoint, not a beta or full W3 completion. The W2 WPF/SCM window still uses its explicitly labelled fixture backend; it does not start this real engine yet.

## Implemented

- Official Wintun packet endpoint connected to the existing mobile gVisor QUIC/HTTPS forwarding engine. No native FD reinterpretation or local unauthenticated proxy.
- Explicit physical IPv4 interface binding (IP_UNICAST_IF); numeric VPN endpoint, normal certificate validation and client identity.
- Up to 16 explicit IPv4 host routes (/32). Default routes, IPv6, endpoint/self routes are rejected. The native adapter has a fresh GUID; all address/route changes belong to that adapter and use ActiveStore. Shared DNS/default routes are untouched.
- Wait for Preferred address state before announcing running. Verify Windows' effective route for every target selects the owned adapter; reject/clean up conflicts instead of silently using direct traffic.
- Start/Stop use the existing controller. Partial failure and repeated Stop clean owned resources. The driver removes the ephemeral adapter when the process dies; no shared snapshot restoration or adoption of existing adapters.
- JSON traffic counters and connection events. Config files are bounded to 1 MiB; errors do not dump identity material. Test credentials are kept in ACL-protected directories, never in the repository.

## Runtime evidence

Windows 10 Pro x64 build 19045. Go 1.26.8 cross-build on Linux. Wintun 0.14.1 copied for local testing from the already installed Netbird runtime, Authenticode Valid / WireGuard LLC; Netbird was not modified. Official archive downloads stalled, so this is not a redistributable runtime package.

Final native result: `C:/Program Files/QUIC Lab Engine MVP 4B3BDCD4-679ea5fc/result.json`.

| Check | Result |
| --- | --- |
| QUIC real Wintun HTTPS + DNS over UDP and TCP | PASS, two runs |
| HTTPS real Wintun HTTPS + DNS over UDP and TCP | PASS, two runs |
| Expected server exit 168.119.242.240 | PASS, checked explicitly |
| Incorrect TLS verification name | Rejected before routes, PASS |
| Conflicting local host route | Rejected before reporting running, PASS |
| Forced termination of exact engine child process | Own adapter removed, PASS |
| Default IPv4 routes and DNS before/after | Unchanged |
| Remaining owned adapters | Zero |
| Windows native unit suite | 9 tests, 10 iterations PASS |
| Linux race: windowsengine, vpnclient, windowsclient, mobile | PASS |
| Windows vet: windowsengine, windows-vpn-engine | PASS |

Initial live test reproduced immediate `unreachable host` while the new adapter address was not ready. Setting DAD policy before address creation and waiting for Preferred fixed it. The TLS negative harness initially wrote a UTF-8 BOM using Windows PowerShell 5.1, causing config rejection rather than reaching TLS; the harness now writes UTF-8 without BOM and the actual TLS rejection passed.

A broader `GOOS=windows go vet ./mobile` attempt fails in an existing test dependency: `internal/awgserver/worker.go:195: w.listenReload undefined`. Server reload implementation is Linux-specific. This is not counted as a passed Windows mobile test suite; the real engine builds and mobile race suite passes on Linux.

## Run

Use an elevated console and the protected test executable, with a private JSON settings file. Example (PowerShell):

```powershell
& 'C:/Program Files/QUIC Lab Engine MVP 4B3BDCD4-679ea5fc/quic-windows-engine.exe' `
  -config 'C:/Program Files/QUIC Lab Engine MVP 4B3BDCD4-679ea5fc/private/quic.json' `
  -duration 2m -probe -expected-exit 168.119.242.240
```

`-probe` requires `1.1.1.1/32` in targets, validates the observed exit IPv4, performs HTTPS and DNS UDP/TCP, then stops. Without `-probe`, the engine forwards only configured targets until Ctrl+C or the duration (1 second to 30 minutes). A console window is appropriate for this deliberate manual invocation; automated test processes use hidden windows. Test configs refer to the dedicated windows.beta.test server account.

Settings keys: transport (`quic`/`https`), endpoint (numeric IPv4:port), hostname, certificate, key, optional ca/server_name/verify_name, interface_index, targets. Determine the intended interface explicitly; do not reuse another computer's index. Private profiles are not example credentials.

Reproduce the elevated matrix with `scripts/test-windows-network.ps1 -Source <prepared-private-artifact-dir> -ExpectedHash <SHA256>`. It copies into a fresh protected Program Files directory, checks engine hash and DLL signature, tests both protocols twice, TLS rejection, route conflict, and process-death cleanup. It retains binaries/logs/private test profiles for audit and never deletes third-party resources. Its currently pinned expected exit is fixture-specific; update only after independently checking the server exit. `-SecurityOnly` skips positive network tests and is not a substitute for the complete matrix.

## Limits / next integration

No all-traffic mode, IPv6 protection, kill switch, automatic reconnect, sleep/resume or OS restart acceptance. After process death the owned routes disappear and ordinary routing resumes: cleanup is not leak prevention. Routes can be changed by another administrator after the startup check; continuous enforcement belongs to W3 filtering. System DNS is unchanged; only explicit DNS addressed to a configured target is tunneled. Arbitrary UDP payload/large-transfer/load matrix remains to be tested; current live UDP evidence is DNS.

Startup cancellation during the existing mobile Gateway.Start may wait for its bounded connection timeout. No guarantee of instantaneous Stop during that stage. User-driven subnet selection, production profile import, WPF/service integration, AWG/VLESS and distributable installer remain subsequent work. Full W3 must add policy enforcement and its network failure matrix before claiming a safe whole-machine VPN.

Review: two Important findings (effective route selection and false-positive direct probes) fixed with RED/GREEN regressions and final native tests. Reserved adapter-address target also rejected. No further review pass performed; cancellation latency recorded above.

Sources: [Wintun API and signed runtime](https://www.wintun.net/), [Microsoft IP_UNICAST_IF](https://learn.microsoft.com/en-us/windows/win32/winsock/ipproto-ip-socket-options).
