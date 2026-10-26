# Mixed Connection Groups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans or superpowers:subagent-driven-development. Track steps with checkboxes.

**Goal:** Add AWG and mixed transport groups to the existing Android automatic-group implementation, in normal and shared-multiplexor HA modes.

**Architecture:** Normal mode replaces independently owned Gateway clients behind the existing stable MultiRouter/TUN. HA projects VLESS and AWG as HTTPS carriers through external underlays to the same multiplexor; compatible own QUIC/HTTPS carriers may connect to that same exit directly. Never interpret an arbitrary external direct server as the HA exit. Each AWG peer identity has at most one active physical path because WG endpoint roaming prevents independent simultaneous same-peer Wi-Fi/LTE tunnels.

**Tech Stack:** Existing Kotlin Android, Go mobile Gateway/ExitRuntime, AWG userspace netstack, instrumentation and local native integration fixtures.

**Spec:** ../specs/2026-10-23-adaptive-paths-design.md, approved user architecture. User explicitly requested this adapter extension on 2026-10-24; no new product decision or permission required.

## Global Constraints

- Preserve inherited dirty worktree `.worktrees/phase1-vpn`; no blanket staging/reset or publication. Existing branch and isolation retained.
- Root profile owns routing, DNS and selected group mode; candidates contribute credentials and transport-specific settings only. Subscription remains one logical connection; deduplicate identical logical paths.
- Existing LTE permission/budget applies; no radio toggles or changes to Note9 user settings. Native/Android fixtures use synthetic credentials and isolated filesDir AND noBackupFilesDir.
- Ordinary switch may end existing flows; HA keeps one authenticated multiplexor session, never falls back directly outside it.
- Parent alone builds AAR/Gradle and uses ADB. Native worker owns native underlay files, session worker owns VpnSession; parent owns projection/editor.
- Group bounds remain 16 logical sources / 32 profile entries / 4096 candidates, max256 per VLESS source. Membership remains explicitly local-only.

## Interfaces

Each private `_group_candidates` entry retains `connection_id` and VLESS canonical `config` for compatibility. New fields `transport` and `gateway` carry a complete transport-specific Gateway JSON (hostname endpoint unresolved until bound Network resolution). VLESS `config` remains canonical imported profile; AWG uses `gateway.awg_config`. `AdaptiveGroups.gateway(candidate, root)` projects clean candidate config with root routing/DNS and no group recursion. Legacy VLESS entries without `gateway` remain supported.

Native HA envelope adds `underlay_awg_config` alongside existing mutually exclusive `underlay_vless_config`, with `underlay_endpoint` the resolved outer address. Target `endpoint` remains the inner multiplexor. ExitRuntime determines exclusive AWG peer identities from validated configuration; it must not admit duplicate peer engines across physical paths or duplicated profiles.

## Review Focus

1. Stale old-client health/events cannot switch or declare healthy a newer candidate; closing an exit during acquisition closes new resources without attaching.
2. Failed initial candidate and never-healthy AWG both eventually try other candidates; no endless waiting on IsConnected() engine initialization.
3. No inherited member credentials, SNI, DNS, server endpoint or transport flags leak into another candidate; root routing/MDM restrictions remain authoritative.
4. HA private gateway DNS is resolved only inside the underlay, budget charged on encrypted outer socket once, no direct fallback on underlay failure.
5. Same AWG peer across profiles/networks is exclusive; removed path resources stop before replacement can acquire its peer.

## Task 1: Native AWG HA carrier

Files: mobile/gateway.go, gateway_bond.go, new gateway_bond_awg.go/tests, mobile/exit_runtime.go and relay tests.
- [x] RED actual AWG dialer and invalid-envelope tests; exclusive-peer admission tests; real local fixture for shared session across mixed carrier failover.
- [x] Implement protected/budgeted AWG userspace dialer, idempotent lifetime, shared gateway/capability validation and peer exclusivity.
- [x] GREEN native targeted regression. Standard mobile suite Windows limitation recorded; use actual selected Go files excluding Linux-only public_awg_test.go when needed.

## Task 2: Candidate projection and editor

Files: AdaptiveGroups.kt, VlessRelayRuntime.kt, VpnConfiguration.kt, ProfileEditorActivity.kt; AdaptiveGroupsIntegrationTest and mixed projection tests.
- [x] RED AWG-root, mixed VLESS/AWG/QUIC/HTTPS candidates and root routing preservation; HA AWG underlay same target, reject wrong direct target, missing credentials and unsupported types.
- [x] Extend typed candidate projection and editor. Preserve existing VLESS canonical keys/tests and MDM selection rights.
- [x] GREEN instrumentation after native AAR rebuild, no device real credentials exported.

## Task 3: Normal mixed lifecycle

Files: VpnSession.kt, optional new MixedGroupSession helper and its tests, group event labels.
- [x] RED deterministic lifecycle tests: first candidate failure, failure before first useful health, failover after health loss, stale old callback, late close, profile switch preserving router/root rules.
- [x] Reuse serialized worker and VlessGroupPolicy fair selection. Replace independently owned Gateway behind existing attached/detached callbacks; preserve TUN and close old flows. Reflect active transport dynamically for network migration; do not use old AWG/VLESS flags after switch.
- [x] GREEN instrumentation/native router regression; test on Note9 without altering real profiles.

## Task 4: Integrated verification

- [x] Build AAR, assembleDebug, assembleDebugAndroidTest, lintDebug using JDK17/SDK cached offline setup.
- [x] Install -r on Note9 and run isolated mixed-group plus existing group/probe/MDM regression classes. Optional real AWG test only with existing authorized fixture; no invented live claims.
- [x] Independent code review, fix important findings with RED/GREEN, document actual test results and remaining limits under docs/testing/.

## Rulings

This extends the already approved design; preserve implementation language/runtime and established normal/HA semantics. New adapters do not require re-running product approval. Reuse existing dirty worktree; commit separation deferred because inherited source edits are interleaved. Plan/ledger lives in ignored artifacts/mixed-groups on Windows where shell-only skill helpers are not available; explicit commands retain equivalent RED/GREEN evidence.

### Integration clarification

`_exit_runtime.target_endpoints` maps each allowed own carrier protocol to the chosen exit's original endpoint (HTTPS and QUIC ports may differ). Its non-empty presence selects the shared-session runtime even when all carriers are direct own-server connections. Every direct candidate must match the chosen target endpoint and authenticated identity. Per-network `underlay_endpoints` holds resolved outer destinations for both underlays and direct carriers; immutable originals remain available for target validation.
