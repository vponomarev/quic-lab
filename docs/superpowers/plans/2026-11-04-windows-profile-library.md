# Windows profile library implementation plan

Use executing-plans inline; one final fresh reviewer, no implementer agents.
Spec: docs/superpowers/specs/2026-11-04-windows-profile-library.md.
Base cd841e8; reuse phase1-vpn. Preserve unrelated dirty work. Source-only tests
on authorized root@192.168.5.214; no Go tests on Windows, native cross-built tests
allowed. Keep local branch; no install/push/merge. Go and WPF, no new dependencies.

## Task 1: service library
- [x] RED profile_library_test.go: save draft without Stop; replace/delete,
  revisions/conflicts/persistence failure; activate keeps local routing and
  app policy; normal Apply retains snapshots; redacted IPC; bounds/invalid IDs.
- [x] Implement profile_library.go, SettingsDocument profiles, cloned document
  and validation, preserve library through Apply. Refactor gated apply helper.
  Add strict list/save/delete/activate IPC and capability profile-library-v1.
- [x] Extend native encrypted-store roundtrip. Run package race on Linux and
  cross-built native Windows tests. Expected all tests PASS.

## Task 2: WPF
- [x] RED acceptance for capability-gated library, save/reopen and stopped
  activation. Implement typed library client + Profiles tab, draft preservation,
  revision updates, error/confirmation flows. Run full WPF acceptance/build.

## Task 3: completion
- [x] Full Go suite, targeted race/vet, native tests and WPF release artifact;
  source hash comparison, one fresh final review and one RED/GREEN fix pass.
- [x] Update roadmap/usage/report, scoped local commit; preserve worktree.

## Review Focus
Credentials escaping through embedded SettingsDocument; partial document writes;
library edits versus hot-policy/recovery/Stop; revision conflicts on activation;
local route/policy preservation across transport changes; old-service and first-run
behavior; dirty draft and lost IPC replies; limits and invalid/stale profile IDs.
