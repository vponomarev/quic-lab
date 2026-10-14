# Windows settings, AWG and VLESS implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax for tracking.

**Goal:** Edit the real Windows profile and up to 256 IPv4 routes through WPF, then support standalone AWG and VLESS.
**Architecture:** Keep authenticated service IPC and owned Wintun routes. Add a revisioned settings manager and encrypted store; replace the fixed runtime backend only after successful Stop/cleanup. Transport adapters share routing and lifecycle, while imports remain protocol-specific.
**Tech Stack:** Go, existing Windows SCM/named pipes/DPAPI, WPF .NET 10, existing AWG and Xray-backed VLESS packages.
**Spec:** docs/superpowers/specs/2026-10-14-windows-settings-transports-design.md

## Global constraints

- One stored connection; 1–256 nonoverlapping IPv4 prefixes, no default route or IPv6, existing reserved-address protections.
- Editor first, AWG second, VLESS third. Existing production/server profiles must remain intact.
- No MDM, app selection, kill switch, subscriptions, or arbitrary executable/config inputs.
- Secrets omitted from status/read responses; unchanged secrets preserved explicitly. Source files read by normal-user GUI, never privileged arbitrary-path reads.
- Apply requires matching saved revision, completed cleanup and atomic encrypted storage. Active and saved revisions distinguished.
- Use current isolated worktree. Go race tests on Linux test host; real Windows networking limited to approved test destinations. Do not reroute the machine's control connection.

## Review focus

- A second stale editor cannot overwrite newer credentials/settings (Task 2 conflict test).
- Failed save after Stop must leave the previous durable profile and report VPN stopped (Task 2 failure injection).
- Import containing default routes, DNS or unsupported protocol parameters must not silently change Windows networking (Tasks 5/6 import tests).
- All 256 routes must survive serialization, restarts and cleanup; 257 must be rejected (Tasks 1/4).
- Service restart after migration cannot replace edited settings with the old engine.json (Task 3 migration test).

## Task 1: Route validation and editable profile contract

Files: internal/windowsengine/engine.go, settings.go, engine_test.go; new internal/windowsengine/settings_test.go.
Interface: Settings remains runtime input. Add Name string and exported MaxTargets=256; retain strict JSON parsing. Produce field/row-aware validation errors suitable for safe UI rendering.

- [ ] Add TestSettings256Targets (256 disjoint /32 accepted; 257 rejected), duplicate/overlap, endpoint overlap, noncanonical CIDR, IPv6, empty/default/reserved routes, plus unchanged existing profile parsing.
- [ ] Run targeted tests on Linux and observe the 256 case fail with current limit 16.
- [ ] Implement the limit and structured validation without changing platform routing semantics; field errors contain no credential values.
- [ ] Run windowsengine tests/race and commit this independently testable change.

## Task 2: Revisioned settings manager and IPC

Files: new internal/windowsclient/settings_manager.go and settings_manager_test.go; protocol.go, protocol_test.go; internal/vpnclient/controller.go only if lifecycle abstraction requires it.
Interfaces: SettingsDocument{Revision uint64, Settings windowsengine.Settings}; SettingsStore.Load/Save(context.Context, SettingsDocument); SettingsManager.Read, Validate, Apply. IPC commands read_settings/validate_settings/apply_settings use a separate typed settings payload with ExpectedRevision and Reconnect, not fixture Config. Response adds capabilities, sanitized settings and saved revision while preserving existing status contract.

- [ ] Write tests for stale revisions, keep/replace secret semantics, failed save, cleanup refusal, concurrent Start/Apply, secret-free read/status/errors and unsupported commands on fixture backend.
- [ ] Observe failures; implement manager locking and backend factory. Validation precedes stopping; successful cleanup precedes persistence/backend replacement. Start uses the same serialized manager, Stop can still cancel in-flight startup.
- [ ] Preserve old start/stop/status behavior and fixture tests; reject unrelated extra payloads. Return machine-readable field errors and explicit conflict/stop/save errors.
- [ ] Run protocol/controller/manager race tests and commit.

## Task 3: Persistent real settings and migration

Files: new internal/windowsclient/settings_store_windows.go; store_windows.go, service_windows.go, backend_windows.go; native_windows_test.go.
Interface: encrypted SettingsStore under the existing service data directory, with a distinct versioned filename; reuse restrictive directory/DPAPI and atomic replacement helpers. Service construction loads settings before creating manager/backend.

- [ ] Add native tests for encrypted roundtrip, unchanged secrets, failed writes, migration from legacy engine.json exactly once, corrupt new store refusing fallback and foreign-user denial.
- [ ] Observe failures, implement store and one-time import. Keep legacy source protected; never log its content. On invalid new store stop with a clear service error, do not silently reset.
- [ ] Wire real service to manager; fixture mode retains its existing ConfigStore. Test clean startup and restart with edited profile.
- [ ] Cross-build service/engine, run native storage/IPC checks, commit.

## Task 4: WPF editor and real service acceptance

Files: new desktop/windows/QuicLab.Windows/SettingsWindow.xaml, SettingsWindow.xaml.cs, SettingsDraft.cs; MainWindow.xaml(.cs), PipeClient.cs; QuicLab.UiTests/Program.cs and new SettingsAcceptance.cs.
Interface: typed SettingsReply/SettingsDraft; extend authenticated PipeClient with settings requests using the same peer verification and bounded framing. Main window opens SettingsWindow; fixture editor remains test-only.

- [ ] Add model/UI tests for unchanged-close without prompt, edit/cancel, multiline CIDR import with row errors, 256 rows, masked unchanged key, unavailable old-service capability and conflict feedback.
- [ ] Implement two-section editor, name/transport/endpoint/TLS fields, file-based cert/key replacement, physical adapter selector in advanced options, fixed Save/Cancel controls and validation summary. Draft changes do not alter runtime.
- [ ] Save while running asks reconnect confirmation. Display progress, distinguish durable save from connection result; reload on success, preserve draft on validation failure.
- [ ] Build/run WPF tests; inspect compact main window and long editor. Update installed service/UI with UAC, verify existing profile migration, safe route edit, reconnect, QUIC/HTTPS TCP/UDP/ICMP and counters, restart persistence.
- [ ] Exercise 256 test prefixes in an isolated native test with deterministic cleanup, not public traffic to 256 destinations; compare routes before/after. Record results and commit.

## Task 5: AWG adapter and config import

Files: internal/windowsengine/engine.go, tun_windows.go; new awg_windows.go, awg_settings.go and awg_settings_test.go; existing mobile/gateway_awg* reused behind platform boundaries; SettingsDraft/SettingsWindow and native acceptance.
Interface: tagged AWG transport settings parsed through existing standalone config parser; transport-specific adapter satisfies Gateway lifecycle, shared Link owns Windows routes. Import returns validated settings and proposed routes separately.

- [ ] Add tests for valid standalone config, missing/invalid keys, unsupported required fields, script directives, DNS/default-route suggestions, AllowedIPs not silently replacing editor routes, endpoint overlap and Stop cleanup on failed handshake.
- [ ] Implement Windows AWG socket/interface bindings and packet forwarding using existing Go core. Keep physical endpoint outside TUN; support endpoint hostname resolution before routes with validated IPv4 results. Report actual protocol/address readiness separately from mere service start.
- [ ] Add import/file UI and supported fields; preserve QUIC profile until user saves replacement. Run tests and Windows builds.
- [ ] On dedicated test credentials/server verify TCP/UDP/ICMP where supported, exit identity, counters, cancellation, wrong key, repeated Start/Stop and settings restart. Commit evidence and implementation.

## Task 6: VLESS adapter and URI import

Files: new internal/windowsengine/vless_windows.go, vless_settings.go, vless_settings_test.go; tun_windows.go; internal/vless existing parser/socket interfaces; SettingsDraft/SettingsWindow and native acceptance.
Interface: validated VLESS config consumes existing ParseImport; Gateway adapter forwards TCP/UDP through existing Xray core, never our ICMP stream extension. Socket factory binds physical interface and resolves endpoint before route installation.

- [ ] Test TLS and REALITY URI imports, supported flow/fingerprint, invalid UUID/key, unsupported WS/gRPC/XHTTP/subscription, endpoint hostname, and no arbitrary outbound JSON execution.
- [ ] Implement adapter and protocol fields; unsupported ping is explicit with no synthetic success. Share route ownership and lifecycle with previous transports.
- [ ] Run Go tests, Windows build and dedicated real TLS/REALITY TCP/UDP, exit/counter, wrong credentials, unavailable endpoint, Stop/cancel and persistence checks. Confirm QUIC/HTTPS/AWG regressions remain green.
- [ ] Commit implementation and transport-specific evidence.

## Task 7: Integrated review and delivery

Files: docs/testing/windows-subnet-preview.md, new docs/testing/windows-settings-transports.md, packaging scripts only as required for new binaries.

- [ ] Review migration, secrets, concurrent lifecycle and cleanup across all transports; fix material findings with regression tests.
- [ ] Run focused full package race/native/UI regression and inspect final GUI. Verify adapters/routes cleanup and preserve user's saved profile after tests.
- [ ] Record supported import variants, 256-route limit, capability differences and deferred items. Install tested build for user acceptance; do not claim public release artifacts updated unless rebuilt/uploaded.
- [ ] Commit and push the feature branch; report implementation/test results and any unavailable live protocol check separately.
