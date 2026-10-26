# MDM transport selection verification

Implemented delegated QUIC/HTTPS/AWG selection alongside profile selection, without pausing MDM. Transport must have provisioned credentials and endpoint. VPN must be stopped, matching profile selection policy. General configuration writes remain forbidden.

Red/green: initial sync preservation regression failed (https reset to quic). Legacy endpoint round-trip regression failed (original quic disappeared). Both pass in final device run.

Final build: assembleDebug, assembleDebugAndroidTest, testDebugUnitTest and lintDebug succeeded using JDK17 and local Gradle 8.11.1. Existing deprecation/SDK metadata warnings remain. Kotlin tests in this project primarily run as instrumentation.

Note9 dc69eb2c: 42 reported tests across MdmProfileSelectionTest, MdmConfigurationTest, MdmConfigurationStoreTest, MdmTelemetryTest, MdmLifecycleTest, MdmConsentTest, MdmUpdateRecoveryTest. 39 passed, two update-fixture opt-in tests skipped, one UI test failed because MdmEnrollActivity did not reach RESUMED while system notification/lockscreen retained focus. Awaiting user unlock for UI verification. No assertion weakened or failure hidden.

Passing integration test confirms transport/profile choice leaves MDM active, local generation unchanged and one pending radio sample intact; general editor remains locked and policy revocation rejects further selection. Encrypted tests cover restart, sync, server override and current/external layers. Review found one legacy endpoint defect, fixed and re-reviewed with no blockers.

Pause now requires confirmation explicitly describing stopped collection and irreversible deletion of pending radio data. Cancel has no pause callback. Visual and UI acceptance pending unlocked device.

Artifacts: artifacts/mdm-transport-final-build.log, artifacts/mdm-transport-final-device.log. APK installed on Note9, not published. No changes to production or Note12.


## Completed UI acceptance

After the user unlocked Note9, MdmConsentTest exposed a stale six-checkbox expectation (the UI now has seven, including direct research). Updated the test to assert the explicit set of seven rights and none preselected, preserving the consent guarantee.

Added MdmPauseDialogTest on an unbound test phone: temporary local MDM state, no server request; show actual pause dialog, verify both warnings, cancel, assert unchanged active state and retained queued sample, restore original state in finally. Initial instrumentation activity launch stalled under background launch restrictions; changed the test harness to the existing shell/lifecycle launch pattern. Product code unchanged in this follow-up.

Final acceptance: **OK (41 tests)** across profile selection, configuration adapter/store, telemetry, lifecycle, consent and pause dialog. No skipped tests or failures in this final run. Log: artifacts/mdm-transport-acceptance-device.log. The earlier update-fixture tests remain outside this focused run, as they require explicit two-stage opt-in.

UI acceptance is complete. Candidate remains installed on Note9; no public release or Note12 install performed.
