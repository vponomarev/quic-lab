# Phone MDM import implementation plan

> Execute inline with superpowers:executing-plans; one final independent review.

**Goal:** import portable settings/keys into a phone MDM draft and verify real HTTPS provisioning.
**Architecture:** native picker/decrypt/prepare, existing shared forms and server-authoritative submit.
**Stack:** Kotlin, packaged JavaScript, existing Go configtransfer and MDM handlers.
**Spec:** ../specs/2026-11-05-mdm-import-design.md

## Constraints
- At most 2 MiB input; credentials require age; no secret persistence/logging.
- No local apply or automatic server save; retain policy and revision baseline.
- Existing worktree; Go tests only Linux; source-only authorized test host transfer.

## Tasks
- [x] 1. Add MdmUserImport helper/tests: decode/prepare/mapping over draft, policy
  preservation, malformed/encrypted/cancel paths. RED compiler/API then device GREEN.
- [x] 2. Add Activity native import coordinator and shared JS button/bridge;
  document picker/password/mode/mapping/cancel, worker lifecycle and stale-session
  fencing. Node + instrumentation RED→GREEN, APK/lint.
- [x] 3. Add opt-in disposable Linux HTTPS fixture + Android wire test exercising
  real editor save/apply/ack and permission denial. Run it on Note9; run relevant
  regressions, one independent review/fix pass, document/commit evidence.

## Review Focus

Completed evidence: [acceptance](../../testing/mdm-import-2026-11-05.md),
[ledger](../../testing/mdm-import-2026-11-05-ledger.md). One review/fix pass.
- Revoke or pause while picker/decrypt is open cannot apply stale config.
- Imports never self-grant policy or expose secrets through error/preview paths.
- Replace/remapped group references remain valid with server-owned selection policy.
- Canceled picker/password/mapping leaves draft and runtime intact; memory cleanup.
- Real HTTPS tests prove server behavior rather than mock-only success, without production changes.
