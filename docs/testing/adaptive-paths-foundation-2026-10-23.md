# Adaptive paths: foundation checkpoint, 2026-10-23

Plan: [adaptive paths foundation](../superpowers/plans/2026-10-23-adaptive-paths-foundation.md).

## Delivered in the working tree

- Subscription refresh recognizes legacy deterministic IDs using the stored canonical node identity and group ownership. Keeps the selected profile ID, name, route/app settings and relay references while updating credentials; does not coalesce a new WebSocket node with an old TCP node at the same endpoint.
- Selected-key normalization and probe membership recognize retained legacy IDs, preserve temporarily missing selections, and exclude other subscriptions' profiles.
- Narrow membership snapshots preserve WebSocket fields and avoid depending on whole-configuration export, which rejects private CA profiles. They are transient identity-matching data, never public diagnostics/export.
- Aggregate probe lookup is shared by startup and periodic configuration revalidation. Reordering sources does not invalidate the aggregate; changed credentials/membership does.

## Evidence from this session

Device: ADB serial `dc69eb2c`, Redmi Note 9 Pro. Installed debug app/test APK using `install -r`; no uninstall, user profile edits, radio changes or production server operations.

Baseline: Android assembleDebug/assembleDebugAndroidTest passed; 17 existing subscription/store instrumentation tests passed.

RED: `legacyRefreshKeepsSelectedProfileAndLocalRules` failed on-device with `expected:<1> but was:<2>`. GREEN: all 14 subscription tests passed after the identity fix.

New selection/grouping, aggregate lookup, and isolated membership snapshot contracts were written before implementation; compilation failed on the missing APIs. Their subsequent behavior tests passed on-device. Aggregate cancellation is established by the shared production lookup and model regressions, not a live network continuity test in this checkpoint.

Final command: Gradle `assembleDebug assembleDebugAndroidTest lintDebug --console=plain --offline` → BUILD SUCCESSFUL. Instrumentation classes `VlessSubscriptionsTest`, `VlessProbeStoreTest`, `VlessProbeCoordinatorTest`, `VlessProbePolicyTest`, `VlessProbeMdmTest` → **OK (30 tests)**. Independent fresh-context review found no remaining actionable findings in the incremental changes. `git diff --check` passed (existing LF/CRLF normalization warnings only).

Debug APK SHA-256: `C4E2AC8BF591D4E11F8319B7010682AFABA86B029BFB05F715215B64C3AA2B76`.

This APK retains the inherited `0.9.5-pre.10` development version metadata; it is not a new published release. Native AAR was the existing worktree artifact; no Go/native implementation changed in this checkpoint and it was not rebuilt here.

Logs and baseline inherited diff: ignored `artifacts/adaptive-paths/`. The source changes remain uncommitted together with the preserved incoming work. Do not commit whole touched files without reviewing the inherited changes they also contain.

## Remaining work

This is only the foundation sub-plan. Adaptive normal/HA groups, transport adapters, two-network probe orchestration/storage/MDM capability, availability-map UI, Windows and Linux work remain. No new failover, live LTE quality, throughput, or session-continuity claims are made.

Full instrumentation suite was not run: some classes require live relay fixtures and mutate VPN state. Selected suites use synthetic profiles and isolated storage. Generic configuration export still reconstructs standalone VLESS as TCP and rejects custom CA; these pre-existing export limitations are not fixed by the narrow membership snapshot and require their own regression-driven follow-up before claiming full WS configuration-transfer support.
