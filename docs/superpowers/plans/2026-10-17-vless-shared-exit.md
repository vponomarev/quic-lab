# VLESS Shared Exit Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Preserve Android TCP connections and gateway exit IP across replacement of a VLESS underlay, using a primary and a joined warm reserve.

**Architecture:** Reuse bond, bondhttps, gateway session registry and ExitRuntime. Inject independent VLESS dialers below the existing authenticated HTTPS carrier; preserve the shared mux across underlay failures.

**Tech Stack:** Go, existing Xray VLESS client, mTLS/WebSocket, Kotlin/Android VpnService, current MDM configuration API.

**Spec:** ../specs/2026-10-17-vless-shared-exit-design.md

Status: implemented and published as 0.9.5-pre.5; Note9 acceptance complete. Note12 field activation pending. See execution reconciliation below for actual coverage; original detailed checkboxes are retained rather than asserting unperformed test combinations. Execution method: inline in the existing isolated phase1-vpn checkout; independent review after functional checks. Do not touch unrelated untracked files or the active Note12 profile.

## Global Constraints

- Default and initial release pool size: 2 joined paths, at most 1 in-flight dial; distinct VLESS candidates when available.
- Internal carrier HTTPS with mTLS; no direct fallback, redirects or plaintext session secrets.
- Existing retention defaults: 120-second pathless grace, 30-second unacknowledged-record timeout, 1024 pending records/session, 64/flow.
- Preserve routing/apps/DNS consent and physical LTE accounting; no double counting inner bytes.
- Capability vless_relay_v1; settings.relay_exit_profile_id references the existing gateway profile, never raw credentials.
- Lab acceptance: warm failover resumes useful data within 3 seconds; all-path outage of 10 seconds retains the same TCP connection. Mobile-network SLA is not asserted.
- No edits to live Note12 until the tested build is ready and user schedules its activation.

## Review Focus

- Blackholed direct access to gateway must not prevent setup through VLESS (Task 1).
- Warm standby must be joined and data-verified, not merely TCP-connected (Tasks 2–3).
- Pending MDM identities, deleted referenced profiles and rollback must not resolve a different gateway identity (Task 4).
- LTE blocked or exhausted during concurrent dial must not leak traffic or silently create another path (Tasks 2, 5).
- A healthy-looking new connection must not conceal destruction of the original TCP socket (Tasks 3, 6).

### Task 1: Injectable authenticated HTTPS carrier

**Files:** mobile/gateway_bond_https.go; new mobile/gateway_bond_vless.go and mobile/gateway_bond_vless_test.go; existing internal/bondhttps/path_test.go.

**Interfaces:** Internal type bondStreamDialer = func(context.Context,string,string)(net.Conn,error). Extract Gateway.addBondHTTPSWithDial(ctx context.Context, endpoint string, info bond.PathInfo, binder SocketBinder, create bool, dial bondStreamDialer, cleanup func()) error. Existing addBondHTTPS remains a direct-dial wrapper. The VLESS adapter supplies Client.DialContext and closes only its own client on path cleanup.

- [x] Write tests: direct wrapper compatibility, VLESS carries inner mTLS successfully with direct gateway dial forbidden, wrong inner identity rejected, redirect rejected, canceled handshake cleans engine/sockets exactly once.
- [x] Run focused Linux tests and observe failure before implementation.
- [x] Extract shared HTTPS handshake without changing BondHello/Welcome or session ownership; meter only outer sockets. Route gateway capability preflight through the same supplied dialer, reusing current validation.
- [x] Run `go test -race ./mobile ./internal/bondhttps -run 'Test.*(Bond|VLESS)' -count=1`; all pass.
- [x] Commit adapter and tests.

### Task 2: Common session with VLESS path pool

**Files:** mobile/exit_runtime.go, mobile/exit_runtime_test.go, mobile/gateway_bond.go, mobile/gateway_exit.go, mobile/gateway_bond_vless.go; internal/vpnmodel/config.go and config_test.go; internal/pathpolicy/pool_test.go.

**Interfaces:** Preserve ExitRuntime.Start/UpdateNetwork/Close and exitTransport signatures. Extend each runtime gateway entry with optional canonical VLESS underlay configuration; outer logical transport remains HTTPS. Candidate identity is stable across retries; path generation changes on every attempt. One Gateway/mux belongs to the logical exit, not the candidate.

- [ ] Add tests for one Create and subsequent joins, two distinct ready candidates, primary loss with same mux/token, third candidate replacing reserve, all failed with bounded backoff, no direct fallback and no immediate failback oscillation.
- [ ] Add budget and lifecycle tests: LTE disallowed mid-dial, late callback after Stop, network generation replacement, one available candidate, all candidates blocked. Assert at most two joined and one dialing.
- [ ] Observe RED using `go test ./mobile ./internal/vpnmodel ./internal/pathpolicy -run 'Test.*(Relay|Pool)' -count=1`.
- [ ] Extend existing pool admission and path identity handling. Reuse data-plane progress and PreferPath; keep session alive while paths are removed/replaced. Do not reuse standalone SwitchVLESSProfile, which closes the active engine and flows.
- [ ] Run the focused tests then full `go test -race ./mobile ./internal/vpnmodel ./internal/pathpolicy -count=1`; commit.

### Task 3: Prove server-side TCP continuity before Android UI work

**Files:** new mobile/gateway_bond_vless_integration_test.go; internal/gateway/bond_https_test.go; internal/bond/retention_test.go; new scripts/test-vless-shared-exit.sh.

**Interfaces:** Test fixture runs two independently stoppable VLESS proxies, one gateway, and a destination assigning a unique accept ID to each TCP connection. Counters distinguish external carrier reconnects from destination reconnects. No new production wire protocol is required.

- [ ] Write fault tests with one destination connection and sequenced bidirectional payload. Assert unchanged accept ID, bond session ID, observed egress address and exact payload after primary reset/blackhole.
- [ ] Add 10-second all-path outage, ACK loss, slow reader, reserve failure and UDP duplicate/expiry cases. Verify bounded memory and expected timeout failure beyond retention; no hidden destination redial.
- [ ] Run failing fixture, implement only missing lifecycle fixes justified by failures, rerun with race detector.
- [ ] Run fixture in isolated Linux network namespaces on test host. Preserve SSH/host default routes; cleanup only fixture namespaces/processes.
- [ ] Save machine-readable acceptance output and commit fixture/regressions. Android integration is gated on these results.

### Task 4: Configuration, MDM and editor compatibility

**Files:** internal/mdm/config.go, config_test.go, report.go, report_test.go, control.go; internal/admin/mdm_editor.js; Android MdmConfiguration.kt, ConfigurationTransfer.kt, MdmDeviceReport.kt, VpnConfiguration.kt, ProfileEditorActivity.kt; corresponding configuration instrumentation tests.

**Interfaces:** Optional string settings.relay_exit_profile_id in the group profile. Referenced profile supplies internal HTTPS gateway identity. New capability vless_relay_v1. Absence leaves standalone behavior unchanged.

- [ ] Write RED tests for missing/self/cyclic references, incompatible gateway profile, referenced identity removal, old client, pending identity merge, encrypted export/import and rollback. Verify public snapshots omit secrets.
- [ ] Implement whole-document validation and runtime resolution under existing configuration lock. Include referenced identity changes in restart decisions. Preserve original standalone group and original gateway profile.
- [ ] Add Android/MDM selector with labels «Напрямую» and «Через профиль QUIC Lab». Preview explicitly warns initial activation reconnects VPN; no direct fallback.
- [ ] Verify `go test ./internal/mdm ./internal/admin -count=1` on Linux; configuration instrumentation tests on Note9; commit.

### Task 5: Android lifecycle and observable readiness

**Files:** Android VpnConfiguration.kt, VpnSession.kt, LabVpnService.kt, VlessGroupEvents.kt, MdmDeviceReport.kt; mobile/exit_runtime.go; internal/mdm/report.go; internal/admin/mdm_editor.js; new Android VlessRelayTest.kt.

**Interfaces:** Project candidate entries and referenced gateway identity into the existing ExitRuntime configuration. Report bounded relay status (session generation, primary/reserve indices, physical network, ready flag, termination reason) only under applicable permissions/capabilities; never URI/keys.

- [ ] Write RED tests for correct runtime projection, two servers on one Wi-Fi, network handover, exhausted LTE, stopped service, permission removal and stale callbacks. Assert no TUN replacement on candidate rotation.
- [ ] Integrate bound VLESS engines with existing global TrafficBudget and network generation callbacks. Explicitly bypass standalone group rotation when relay mode is enabled.
- [ ] Expose primary/ready reserve/degraded/no-path/session-lost states. Verify report serialization and UI without credentials.
- [ ] Build AAR, assembleDebug/assembleDebugAndroidTest/lintDebug; run targeted Android tests and existing group/bond/budget regressions. Commit.

### Task 6: Device acceptance, review and release

**Files:** new Android VlessRelayDeviceTest.kt; existing android/probe companion as required; new docs/testing/vless-shared-exit.md; release notes/version metadata.

- [ ] Use Note9 with saved/restorable profile and permitted test identity. Run one long-lived companion TCP socket through actual TUN while disabling a controlled VLESS path. Test Wi-Fi/LTE only when device/network permits it; mark untested conditions explicitly.
- [ ] Assert same destination accept ID and exit IP, exact payload, measured failover pause, bounded queues. Exercise UDP and explicit Stop; restore prior profile/MDM state where server access permits.
- [ ] Run required Go race, Kotlin instrumentation and lint checks on final code. Request independent review, fix material issues with regressions, rerun affected checks.
- [ ] Prepare versioned APK/server artifacts and hashes, rollback copies and compatibility checks; deploy with existing publication workflow only after acceptance.
- [ ] Deliver Note12-compatible build. Enable the new field only after its new capability report and agreed activation; verify actual gateway exit and MDM readiness. Record any production limitations rather than claiming seamlessness from a reconnect.

## Self-review

Spec requirements map to adapter (1), scheduling/security budget lifecycle (2), continuity/retention oracle (3), configuration and compatibility (4), device/reporting integration (5), actual acceptance and deployment (6). Public IP continuity and TCP continuity are independently asserted. Temporary direct outage is part of setup testing. No change to existing retention limits or billing accounting is hidden in this plan.

## Execution reconciliation — 2026-10-17

Tasks 1–5 delivered: transport/pool, failure fixtures, configuration gates and selectors, Android runtime/network/status integration. Task 6 lab acceptance, independent review and publication delivered. Evidence and measured limits: [verification](../../testing/vless-shared-exit.md); [release](../../release-0.9.5-pre.5.md).

Existing bond tests were reused for ACK/deduplication, retention, pending limits, slow readers and datagram expiry; these were not all repeated as a physical-phone/VLESS cross-product. Configuration instrumentation covers retained identities and rollback through the existing store tests. Actual Note9 Wi-Fi carrier loss passed TCP and UDP; physical LTE handover and blocked-network third-party subscription behavior remain unverified. No claim of an exhaustive physical network/budget/consent failure matrix.

Remaining acceptance: upgrade Note12, await capability report, agree activation of its shared-exit reference, then verify production exit IP and readiness. First activation reconnects VPN. Do not mark that step complete from lab results.
