# Linux per-app routing implementation plan

> Use superpowers:executing-plans inline; one fresh final reviewer, no implementer delegation.

**Goal:** Linux CLI parity with the Windows per-app TUN/direct slice.
**Architecture:** Shared immutable policy/router, Linux proc owner resolver, protected direct binder.
**Tech stack:** Go, procfs, existing tun2socks, Linux network namespaces, existing transports.
**Spec:** docs/superpowers/specs/2026-11-04-linux-per-app-routing.md
**Base:** d69276d in existing phase1-vpn worktree.

## Global constraints

Source-only transfer to authorized root@192.168.5.214. Preserve unrelated dirty work.
No Go tests on Windows. Keep Windows path semantics and Android APIs unchanged.
No real profile activation, host route capture, new dependencies, eBPF or firewall rules.
User opted for autonomous implementation; choose routine details without new approval gates.

## Task 1 — portable policy and Linux owner

- [x] RED: Linux case-sensitive paths, both lists/unknown/shared behavior, proc table
  parsing and tuple/wildcard matching, missing/inaccessible/changed owner, bounds,
  real loopback ownership. Expected: absent Linux APIs fail before implementation.
- [x] Implement path-key parameter inside shared compiler; Linux-specific canonicalization
  and bounded proc resolver. Expected: package tests and race pass on Linux, Windows
  policy tests unchanged.

## Task 2 — client/CLI

- [x] RED: app_routing JSON decode/validation, disabled compatibility, snapshot output
  and private CLI aggregate events. Expected: new integration behavior absent.
- [x] Wire router into Client with FD ownership/Stop, canonicalize before resources,
  emit app-routing counters and validation preview. Expected: integration/unit/race pass.

## Task 3 — real two-process acceptance

- [x] Add private netns fixture with two copied executable probe processes and
  source-address echo. Verify same target through distinct exits, both modes,
  unknown policies, cleanup, transport variants and unchanged management route.
- [x] Run full Go suite, race/vet, Windows compile/native compatibility. Expected:
  all required tests pass; platform-specific limitations are explicitly recorded.
- [x] One fresh review; Important fixes in one RED/GREEN pass. Update roadmap,
  Linux usage/testing documentation, locally commit scoped files only.

## Review Focus

Incomplete proc visibility and FD races; PID/inode reuse and cross-mount identities;
UDP connected/wildcard/shared attribution; aggregate resource limits across restarts;
TUN borrowed-FD ownership and direct recursion; Windows case folding compatibility;
CLI privacy and truthful capture scope; netns fixture exit evidence versus echo-only tests.
