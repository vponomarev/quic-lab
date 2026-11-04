# Linux VPN readiness implementation plan

Use executing-plans inline, one fresh final reviewer. Spec:
docs/superpowers/specs/2026-11-04-linux-vpn-readiness.md. Base 57fedae.
Reuse phase1-vpn; preserve unrelated dirty files. Go tests only on authorized
Linux test tree; source-only transfer, native cross-built Windows tests allowed.
No install/merge/push. Existing Go dependencies and private namespace fixtures.

## Task 1: probe and state
- [x] RED desktop DNS integrity/cancel/no-direct tests and Linux config/state tests.
- [x] Desktop DNS method and bounded single health worker; config validation.
  Expected focused race tests PASS.
## Task 2: lifecycle and acceptance
- [x] Wire Start/Stop/recovery and CLI readiness. Validate successful recovery
  before exposing VPN generation; health enables recovery without per-app.
- [x] Extend namespace fixture DNS response and script opt-in health branch.
  Verify AWG/VLESS TCP+WS loss/recovery, direct/TUN preservation and Stop.
## Task 3: completion
- [x] Full suite, targeted race/vet, legacy regression and Windows cross-build;
  source comparison, artifacts, one independent review + one RED/GREEN fix pass.
- [x] Update roadmap/usage/report, scoped local commit and cleanup task scratch.

## Review Focus
Probe routing/fallback and reply authenticity; bounded goroutines/cancellation;
health worker versus Stop/replacement; stale successes and false readiness;
retry behavior when resolver alone fails; config opt-in compatibility and secrets
in logs; namespace acceptance distinguishing live underlay from failed VPN.
