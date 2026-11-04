# SDD ledger — plan: docs/superpowers/plans/2026-11-05-mdm-import.md
Base 0732e0b. Existing clean codex/server-exits worktree.
Ruling: continue autonomously under prior authorization and explicit full VPN/import scope.
Ruling: native portable JSON/age import reuses existing validation; raw provider formats remain explicit key fields.
Pre-flight: helper returns draft only; JS retains baseline; existing submit owns policy/CAS/provisioning.
Pre-flight: generic importer validates selection references; remove policy for merge then restore exact original policy for existing server trimming.

Implementation: native SAF picker, password, add/update/replace and mapping operate on a copied server draft only. JS retains the original CAS/policy baseline; explicit Send remains the sole write path.
Helper RED: missing MdmUserImport API/compiler. JS RED: missing import control. Both then GREEN. Helper instrumentation 3/3, UI/helper combined 4/4.
HTTPS acceptance: isolated Linux production Store/NewHandler fixture, isolated Android context and synthetic keys; grant/import/save/retry/poll/apply/report/revoke passed. Device 1/1 (19.53 s), server 55.36 s. Fixture shut down automatically.
Regression: Linux go test ./..., go vet ./..., MDM/configtransfer/admin race all passed. Node editor/probe passed. Android JVM 2/2, APK/test APK/lint passed. Note9 editor/import/quiet tests 22/22 (15.344 s).
Independent review: one read-only gpt-6-astra reviewer. Critical none. Important: direct native coordinator cancellation/lifecycle coverage absent. Accepted; added six real-coordinator tests with native dialogs, synthetic ContentResolver input and controlled executor.
Test harness diagnosis: MIUI rejected instrumentation background Activity launch before flow execution. Debug-only empty host now launches through shell, guarded by DUMP permission; provider unexported. Bounded monitor and worker waits. Dialog button callbacks are asynchronous; tests now await main-loop idle before assertions.
Additional RED: password dialog lacked FLAG_SECURE even with a secure parent Activity (5/6 tests passed, password-window assertion failed). Added explicit secure flag to password dialog. Final post-fix validation recorded in acceptance report.
Scope remains portable JSON/age; raw provider import, new group authoring, physical VPN traffic/OEM/Doze and release remain separate. No production deployment, push or merge.
Final validation: updated APK installed; all 28 device tests PASS (28.479 s), zero skips, including all six flow tests and password FLAG_SECURE regression. Final APK/instrumentation build, JVM 2/2, lint and Node tests PASS; git diff --check clean. Go source unchanged since full suite/vet/race success. Review Important closed; no deferred review items and no rereview.
