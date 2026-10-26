# MDM transport selection implementation plan

> Execute inline with superpowers:executing-plans.

Goal: delegated profile and transport choice without pausing MDM.
Spec: ../specs/2026-10-27-mdm-transport-selection-design.md

- [x] Add failing regression for preserving a delegated transport through sync.
- [x] Extend MdmProfileSelection and encrypted MdmConfigurationStore with validated transport-only mutation and carry rules; test revocation, unsupported transport, credentials, explicit server changes, restart and detach.
- [x] Expose transport selection beside existing profile selection; maintain VPN-off guard and recheck authority at mutation.
- [x] Confirm MDM pause with warning; cancel must not pause or delete queue.
- [x] Finish unlocked-device UI verification; build/lint, focused logic tests and review are complete.

Preserve all unrelated existing changes. No release or production policy changes in this task.

## Verification ledger

- Red: preservesDelegatedTransportOnUnchangedServerSync failed on Note9 (expected https, got quic).
- First green: 29 profile/store/telemetry/lifecycle tests passed on Note9.
- Review found loss of original endpoint in legacy profiles. Added canReturnToOriginalTransportWithOnlyGenericEndpoint and observed failure; fixed availability/selection to resolve against preserved baseline.
- Build assembleDebug, assembleDebugAndroidTest, testDebugUnitTest and lintDebug passed. Existing deprecated Android API/SDK metadata warnings remain.
- Expanded device run: 42 tests reported: 39 passed, two opt-in update-fixture tests skipped, one UI failure MdmConsentTest.openingInvitationDoesNotEnrollOrGrantPermissions (screen did not resume while device showed system lockscreen). Profile selection, configuration, queue, lifecycle groups completed without failures. Final UI verification awaits unlocked Note9.
- Final code review: no remaining blockers after endpoint fix.
- No release, server policy changes, or Note12 installation performed.

- Unlocked-device follow-up complete: updated stale consent checkbox expectation, added real pause-dialog cancellation test. Final related suite OK (41 tests), no skips/failures. Temporary MDM state restored. See docs/testing/mdm-transport-selection-2026-10-27.md.
