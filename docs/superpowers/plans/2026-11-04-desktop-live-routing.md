# Desktop live routing implementation plan

Spec: docs/superpowers/specs/2026-11-04-desktop-live-routing.md. Base d414279.
Execute inline using executing-plans; one fresh final reviewer, no implementer agents.
Reuse phase1-vpn; preserve unrelated dirty work. Source-only tests on authorized
root@192.168.5.214. No Go tests on Windows. No install/push/merge.

## Task 1: shared router lifecycle

- [x] RED tests in mobile/desktop_app_router_test.go: policy updates affect only new
  flows; ReplaceVPN(nil) blocks VPN and DNS, keeps direct; replacing VPN preserves
  cumulative counters; concurrent Close/update is bounded.
- [x] Add UpdatePolicy(*approuting.TUNCompiled) error and ReplaceVPN(*Gateway) error,
  with nil VPN meaning unavailable, immutable atomic policy and serialized retirement.
  Add TUNCompiled.Enabled() for disabled fast-path. Run focused mobile race tests.

## Task 2: desktop adapters and policy publication

- [x] RED Windows manager tests: app-only save while running without Stop, save failure
  keeps runtime/revision, non-app edit still guarded. Automatic tests: same-uplink
  recovery retains engine; hot policy survives future engine creation.
- [x] Wire NativeGateway, Engine and Automatic; backend ApplyAppRouting(ctx, policy,
  persist func() error) serializes validate/persist/publish. Always own desktop router
  so enabling policy is hot. Preserve existing network-change teardown semantics.
- [x] Linux Client.ReloadApplications(Config) and RecoverVPN(context.Context) retain
  router/TUN; CLI handles SIGHUP and serialized retries. Test rejection of non-policy
  edits, immutable config and shutdown during recovery.
- [x] WPF uses live-apply capability and compares non-app fields before deciding to
  prompt for reconnect; test this decision and update user-facing copy.

## Task 3: acceptance and completion

- [x] Linux private-netns persistent TCP/UDP direct streams, changed new-flow exits,
  rejected reload, same TUN through VPN outage, recovery, explicit Stop cleanup.
- [x] Full Go suite, targeted race/vet, Windows cross-built native tests and WPF
  acceptance/build; artifacts and source hashes. One fresh final review, one fix pass.
- [x] Update roadmap, usage and verification report; scoped local commit, keep worktree.

## Review Focus

Late handler registration after VPN retirement; counters during concurrent swaps;
persist failure/cancellation and settings revision coherence; policy update versus
network reconcile/Stop; no VPN-to-direct fallback; service/UI capability compatibility;
Linux SIGHUP path/permissions, bounded retries and prompt termination during failed dial.
