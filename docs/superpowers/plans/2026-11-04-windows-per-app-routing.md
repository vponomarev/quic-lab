# Windows per-app routing: first executable TUN/direct slice

Binding spec: `docs/superpowers/specs/2026-10-14-windows-per-app-tun-design.md`.
Base: bd0429a. Execute inline using the existing isolated worktree.

## Scope and constraints

Implement opt-in Windows IPv4 application routing with two remembered path lists,
VPN/direct/block unknown policy (default VPN), bounded owner resolution, protected
direct sockets, separate counters, service validation and settings UI. Legacy
settings and Android UID routing retain their behavior. Do not install filters,
change IPv6, activate a user's profile, or transmit executable identities.

First slice uses the existing explicit stop/reconnect settings transaction. Hot
policy replacement and preserving direct flows during automatic VPN engine restart
remain later work; UI must disclose the reconnect. Data plane decisions are pinned
to each flow. No fallback from a failed VPN flow to direct.

## Task 1: policy and Windows attribution

Add version 2 path policy separately from legacy WFP policy. Test both modes,
unknown/shared/mixed ownership, malformed paths and immutable lists. Implement
IPv4 TCP full-tuple and UDP local endpoint/wildcard candidate matching. Bound table
size, candidate count and concurrent native lookups; timeout/overload become
unknown. Query process creation time on every lookup, never cache a PID decision.
Test synthetic table edges and real loopback TCP/UDP owners without network changes.
Expected: Linux package tests and native Windows executable pass.

## Task 2: packet routing and lifecycle

Generalize the existing TUN router boundary without changing Android semantics.
Reuse protected direct dialer and per-exit handlers. Resolve off the packet callback,
bound pending first flows, retain global VPN DNS, publish separate VPN/direct and
unknown/shared counters. Test actual TCP/UDP payloads, large UDP replies, failure
without fallback, DNS override, saturation and cancellation. Expected: focused
mobile tests and race pass; no direct bytes in VPN traffic.

## Task 3: service and UI

Persist optional app policy in settings, deep-copy it and validate/canonicalize paths
in service. Advertise a new capability. UI edits two lists and unknown handling,
previews capture scope and uses existing explicit reconnect transaction. Old services
cannot silently accept application rules. Test settings roundtrip/isolation/errors
and WPF enabled/disabled/legacy behavior. Expected: native service tests, UI build
and acceptance pass.

## Task 4: integrated verification and record

Run full Go suite on the authorized Linux source-only host, relevant race/vet,
Windows cross-build and native tests, WPF tests and Android regression if shared
code changed. One fresh-context final review; one Important fix pass with RED/GREEN.
Update roadmap and verification report, commit only this slice. Do not mark full
physical application acceptance complete without real exit evidence.

## Review Focus

Check Windows table byte order/layout and partial snapshots, PID/port reuse and
inaccessible candidates, uncancellable native-call bounds, queue saturation and
shutdown races, DNS bypass, outbound recursion protection, stale settings aliases,
legacy service compatibility, direct/VPN accounting and failure fallback. Inspect
scope claims against evidence: synthetic stacks do not prove OS capture or Roblox.
