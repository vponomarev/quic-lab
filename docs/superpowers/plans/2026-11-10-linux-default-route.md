# Linux Default Route Implementation Plan

> Execute inline using superpowers:executing-plans; user requested autonomous continuation.

**Goal:** Replace the laptop's 79 routes with a supported Linux VPN default route.
**Architecture:** Explicit /0 capture, preserved physical routes, two /1 routes and physical endpoint exception; reuse transport/app routing engines.
**Tech Stack:** Go, iproute2, Linux namespaces, systemd.
**Spec:** ../specs/2026-11-10-linux-default-route.md

## Global constraints
No production server changes; no keys/profile transfer to the build host.
No changes to physical defaults; two /1 routes work with any default metric.
Existing subnet configuration remains compatible. Preserve root-only profiles.

## Review focus
- Default metric zero or missing default: preserve the physical routing unchanged.
- Endpoint behind default vs connected/specific route: pin only when necessary.
- Existing more-specific routes and bound direct sockets must keep working.
- Partial start and orderly stop must restore routes without deleting foreign ones.
- AWG AllowedIPs must cover /0; mixed /0 lists must be rejected.

## Task 1: Route support and regression tests
- [x] Add failing config and route-planning tests, run on Linux.
- [x] Implement /0 validation, pure route plan and backend lifecycle integration.
- [x] Run Linux client/CLI regression tests and namespace acceptance across transports.

## Task 2: Deployment and documentation
- [x] Update usage docs, inspect diff with independent final review.
- [x] Build Linux binary; back up laptop binary/profiles and migrate full examples.
- [x] Verify start/stop, routing, HTTPS/DNS, all transports, leave QUIC enabled.
- [x] Record results, commit the completed change.

## Execution ledger
- Existing worktree codex/server-exits, clean baseline 120d9e2.
- User's chosen approach and standing autonomous-work instruction cover execution.

- Ruling: use two /1 routes per the user’s explicit suggestion; avoids default-metric changes.
- RED: TestFullIPv4Configuration and TestFullIPv4PreservesSpecificRoutes failed on the prior implementation; both pass after implementation.


- Review found onlink nexthop loss; RED→GREEN test and real /32-underlay acceptance passed after fixing.
- All four transports and subnet mode passed on the laptop; QUIC active/enabled. DNS helper start-limit-hit fixed and retested.
- Final acceptance details: ../../testing/linux-full-routing-2026-11-10.md.
