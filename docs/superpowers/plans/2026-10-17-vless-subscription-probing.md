# VLESS Subscription Probing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Probe imported VLESS nodes from Android, with one result table and controls available locally and through MDM, preserving the working VPN.

**Architecture:** A single Android coordinator owns bounded independent probes, network binding and shared budget leases. Go performs verified exchanges; MDM transports capability-gated jobs and monotonic result snapshots, not configuration revisions. Two UIs consume the same result model.

**Tech Stack:** Go/Xray, Kotlin Android, existing MDM JSON/HTTPS, server-rendered HTML/JavaScript.

**Spec:** ../specs/2026-10-17-vless-subscription-probing-design.md

Status: written for review. Recommended execution: native in this thread, sequential tasks and one independent final review. Reuse the phase1-vpn worktree; preserve unrelated untracked files. Do not activate another Note12 profile or interrupt its working VPN for diagnostic tests.

## Global Constraints

- Maximum 2 simultaneous probes, 256 nodes, 20 seconds per node including DNS, 45 minutes per run; one run/device.
- Three small verified HTTPS echoes; median successful echo RTT and success count. Separate connection duration, total duration and relay outcome.
- Command acceptance TTL 5 minutes; capability `vless_probe_v1` on both sides; no unknown commands to old clients.
- Remote execution requires active MDM and VPN+telemetry rights. No arbitrary remote probe destination or credentials in results.
- Shared durable LTE budget, outer socket accounting only. No automatic VPN start, profile switch, rotation or TUN replacement.
- Last run/source; maximum 32 sources/device, 256 rows/run, 512 KiB/snapshot. UTC wire dates, local UI dates.
- Network/configuration change cancels remaining work with an explicit reason. Process death marks interrupted; no silent replay.
- Local foreground use may run without VPN; remote use without a permissible foreground owner returns needs_foreground.

## Review Focus

1. Delayed cancel must not cancel a newer run; pin to run ID (Tasks 2–3).
2. Candidate reordering or credentials changed under the same display name must invalidate the snapshot, not misattribute results (Tasks 2–4).
3. Probe lease release must not clear the active VPN budget or grant a new LTE period (Task 3).
4. Successful HTTPS through a node must not imply relay authentication succeeded (Tasks 1, 4–5).
5. Pause/revoke/capability downgrade during result delivery must not accept unauthorized snapshots or wedge legacy sync (Task 2).

### Task 1: Verified probe transport and bounded echo endpoint

**Files:** modify mobile/vless_probe.go, mobile/vless_probe_test.go; create mobile/vless_probe_relay.go and mobile/vless_probe_relay_test.go; create internal/probeecho/handler.go and handler_test.go; modify cmd/server/main.go and its public-handler tests.

**Interfaces:** retain NewVLESSProbe and HTTPSStatus compatibility. Add `(p *VLESSProbe) Run(optionsJSON string) (string,error)`; options contain HTTPS target and optional existing gateway config. Result JSON uses `outcome`, `connectMs`, `totalMs`, `echoRttMs`, `echoSucceeded`, `relayOutcome`. Close cancels every operation. Add `probeecho.NewHandler() http.Handler`, exposed at `/api/v1/probe/echo` on the public HTTPS listener, outside client-certificate-required tunnel routing.

- [ ] Add TestProbeEchoContract: POST JSON with 32-byte hex nonce returns matching version/nonce; reject >1 KiB, invalid method/nonce; response <=1 KiB, no-store, no redirect/target forwarding. Bound global concurrent requests to 16 and token bucket to 100 requests/sec (burst 100); reject excess with 429 without an unbounded source-IP map.
- [ ] Add TestVLESSProbeRun: real fixture must reject wrong nonce, truncated/oversized body, redirect and invalid TLS; three echoes expose success count and RTT only when measured; timeout <=20 seconds including cancellation; Close releases sockets. Add relay wrong-identity test with successful HTTPS and failed relay outcome.
- [ ] Run `go test ./internal/probeecho ./mobile -run 'Test.*(Probe|EchoContract)' -count=1` on Linux; record RED assertions.
- [ ] Implement exact-body validation with random nonce, reusable verified HTTPS connection, monotonic timings and sanitized error classification. Use a separate short-lived Gateway/bond for relay; never reuse the working gateway token. Probe its data plane and close it on every return. A stage without an observable boundary omits connectMs and reports totalMs.
- [ ] Run focused race tests plus `go test ./cmd/server ./internal/probeecho -count=1`; verify public endpoint routing with both shared and separate HTTPS listeners. Commit transport/endpoint files.

### Task 2: Capability-gated MDM jobs and bounded result persistence

**Files:** create internal/mdm/probe.go and probe_test.go; modify types.go, store.go, control.go, report.go, http.go; create internal/admin/mdm_probe.go and mdm_probe_test.go; register routes alongside mdm_control.go.

**Interfaces:** `ProbeRequest{SourceID, Fingerprint, Action, RunID}`; actions start/retry/cancel. `ProbeSnapshot{RunID,SourceID,Fingerprint,Sequence,Network,Initiator,State,StartedAt,FinishedAt,Rows}`; rows carry stable node ID/name/endpoint, role, stage/outcome, optional timings, success count, relay outcome and measuredAt. `Store.QueueProbe(id,requestID string,request ProbeRequest,now time.Time)(Command,error)` and `Store.SaveProbe(id string,epoch int64,snapshot ProbeSnapshot,now time.Time) error`. Command optional `probe` payload with kind `vless_probe`; results via optional DeviceReport probeSnapshots, max one snapshot/report. Do not increase configuration revision.

- [ ] Add TestProbeCommands: idempotent request ID, TTL5m, active binding, VPN+telemetry rights, missing capability, downgrade after queuing, stale epoch/source fingerprint, cancel for a prior run. Assert older clients never receive vless_probe commands.
- [ ] Add TestProbeSnapshots: duplicate/older sequence ignored, mismatched binding/epoch denied, rights revoked denied, 257 rows/33 retained sources/oversized snapshot handled by stated limits (oldest source eviction), bounded strings/enums and no raw error details. Keep old accepted results on permission withdrawal.
- [ ] Run `go test ./internal/mdm ./internal/admin -run 'TestProbe' -count=1`; observe RED.
- [ ] Implement DTO validation and cloned durable store updates. Validate source against the current reported configuration for MDM runs; local subscription source is a bounded opaque identifier. Keep network measurement time distinct from receipt time. Add authenticated CSRF POST `/mdm/probe` and read-only GET `/mdm/probes?id=...`; apply existing binding ownership controls.
- [ ] Run full mdm/admin race tests and legacy sync regressions; commit. Do not enable any real-device command yet.

### Task 3: Android coordinator, lifecycle and shared budget

**Files:** create VlessProbeModels.kt, VlessProbeStore.kt, VlessProbeCoordinator.kt, VlessProbeNetwork.kt in android/app/src/main/java/ru/vpnc/quiclab; modify VpnSession.kt and LabVpnService.kt only to expose safe network/protection leases; reuse VpnBudgetRun.kt acquireControl/releaseControl; create corresponding androidTest VlessProbeCoordinatorTest.kt and VlessProbeStoreTest.kt.

**Interfaces:** singleton `VlessProbeCoordinator.start(context:Context, request:ProbeStart):ProbeRun`, `cancel(runId:String):Boolean`, `snapshot(sourceId:String):ProbeRun?`; ProbeStart captures source fingerprint, immutable nodes and local/mdm initiator. ProbeRun/ProbeRow mirror Task2 JSON. `VlessProbeNetwork.acquire(context:Context):ProbeNetworkLease` supplies physical network, binder, shared meter and close; never obtains a separate fresh budget period. Store saves atomically and restores running runs as interrupted.

- [ ] Add tests using fake clock/network/runner: maxActive<=2; 256 accepted,257 rejected; busy and duplicate starts; retry only non-success nodes; deadline20s/node and45m/run; cancellation closes runners; late callbacks ignored; new-run cancel isolation; source/credential reorder fingerprints; network_changed, configuration_changed and process-death interrupted.
- [ ] Add shared-budget lifecycle tests: acquire probe while VPN active, release probe preserves same meter and count; probe with VPN stopped restores saved period; exhausted budget prevents outer socket creation; no double byte accounting. Foreground loss cancels local work only when no existing foreground owner exists.
- [ ] Build/run targeted instrumentation and record RED; implement fixed-size worker pool and serial state transitions, not work on the main thread. Source snapshots read under existing configuration lock, network IO outside it. Normalize source membership fingerprint independent of UI order but include canonical credentials privately in the hash.
- [ ] Integrate Go Run and Close from Task1. Cancel remote work on MDM pause/right withdrawal; suppress result upload immediately. Use existing network protect+bind semantics with VPN, explicit physical Network binding when no VPN; refuse ambiguous VPN-network selection.
- [ ] Run instrumentation, AAR build and lint; commit coordinator/store/network changes.

### Task 4: MDM Android adapter and both tables

**Files:** create MdmProbeControl.kt and VlessProbeActivity.kt; modify MdmModels.kt, MdmController.kt, MdmRuntime.kt, MdmDeviceReport.kt, VlessSubscriptionsActivity.kt, ProfileEditorActivity.kt and AndroidManifest.xml; create internal/admin/mdm_probe.html and mdm_probe.js; modify mdm_editor.html for navigation; create Android VlessProbeMdmTest.kt and server render tests.

**Interfaces:** `MdmProbeControl.execute(context:Context,state:MdmState,command:MdmCommand,serverTime:Instant):String` returns queued/busy/denied/expired/needs_foreground; uses Task3 coordinator. New command parser activated only after negotiated capability. Upload snapshots from Task3 via Task2 report field, at most once/second and on terminal state; retain pending latest snapshot for retry without accumulating history.

- [ ] Add tests for duplicate command, revoked rights, old capability, stale epoch/fingerprint, delayed cancellation, UI recreation, offline result, no telemetry upload; server render checks escaped names/endpoints and separate HTTPS/relay results. Verify no URI credentials in response or HTML.
- [ ] Run focused tests to RED. Implement adapter off the main thread; don't reuse synchronous VPN-start execution for long-running probes. Add capability negotiation on both ends.
- [ ] Implement Android entry points for imported subscription and MDM-only group. Render buttons «Проверить все», «Повторить неудачные», «Остановить», progress, network, local timestamps; compact rows, role markers, filters all/working/errors/unverified and order/RTT sort. No subscription refresh on page entry.
- [ ] Implement matching WEB table with authenticated polling while visible, receipt/measurement time, queued/running/finished/interrupted and old-network indication. Expose unsupported capability/rights and stale device report clearly. Stop polling on page hide; don't trigger probes by viewing page.
- [ ] Run Android instrumentation/lint, mdm/admin tests and `node --check internal/admin/mdm_probe.js`; inspect mobile-width Android and WEB plus desktop 32-row display. Commit UI/adapter.

### Task 5: End-to-end continuity, review and release

**Files:** create Android VlessProbeDeviceTest.kt, extend mobile/relay_android_fixture_test.go with controlled failed node/echo service; add docs/testing/vless-subscription-probing.md and release notes; update Android build.gradle.kts only when final code passes.

- [ ] Add actual Note9 test: two functioning VLESS nodes and one unavailable, concurrent long-lived TCP through working VPN, probe from local and MDM; assert one destination accept ID and exact payload before/during/after probes. Assert success/timeout/relay-auth distinction, stop cancellation and no TUN/profile switch. Test UDP continues as an additional smoke check.
- [ ] Execute on lab host and Note9; preserve original profile, MDM state and working VPN. Record unavailable physical LTE scenarios rather than claiming simulated budget coverage proves mobile-network behavior.
- [ ] Run final `go test -race ./mobile ./internal/mdm ./internal/admin ./internal/probeecho ./internal/bond ./internal/bondhttps -count=1` on Linux; cmd/server tests; AAR, assembleDebug, assembleDebugAndroidTest, lintDebug and targeted existing VLESS/MDM/budget instrumentation. Document outputs and limits.
- [ ] Request one independent whole-change review, reproduce material findings with tests and fix them. Do not publish merely because endpoint/UI tests pass while live session continuity fails.
- [ ] Build versioned APK/server, record SHA-256 and rollback copies, deploy server+APK through existing workflow, verify public APK hash and capabilities. Publish only tested build; Note12 upgrade remains user-controlled. After update, use authorized probe commands without changing its VPN configuration and confirm WEB/local result parity.

## Self-review mapping

Spec purpose/UI maps to Task4; bounded network/HTTPS/relay stages to Task1; limits, permissions, idempotency and retention to Tasks2–3; compatibility/transport delivery to Tasks2/4; actual VPN continuity and publication to Task5. Review Focus cases each have an explicit regression above. Timing metrics never infer an unobservable handshake boundary. Readiness and probe success remain distinct; no scheduling policy changes included.
