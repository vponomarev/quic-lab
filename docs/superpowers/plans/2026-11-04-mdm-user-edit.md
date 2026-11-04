# MDM user editing implementation plan

> Required execution skill: superpowers:executing-plans, inline tasks with one final independent review.

**Goal:** administrator permits a device user to edit MDM configuration via the phone; server revisions remain authoritative.
**Architecture:** dedicated server-owned policy, authenticated device editor API, packaged existing form UI in Android with a narrow native transport bridge, existing provisioning coordinator.
**Tech Stack:** Go, Kotlin/Android, existing JavaScript editor.
**Spec:** ../specs/2026-11-04-mdm-user-edit-design.md

## Constraints
- Deny by default; policy cannot be self-granted; preserve mode and selection policy.
- No local runtime mutation before server provisioning; idempotent bounded requests.
- No secrets in view/audit/errors; no remote WebView pages or general bridge URLs.
- Linux source-only tests on authorized host; no Windows go test; preserve current worktree.

## Tasks
- [x] 1. Server store/API: internal/mdm/user_config.go and tests; add policy state,
  atomic revision/epoch/generation checks, actor-separated idempotency and sanitized
  view. Reuse checked desired commit. Add strict HTTP handlers and capability.
  RED missing APIs then Go/race GREEN; tests cover all authorization boundaries,
  stale drafts, idempotent retries, pending credentials, policy and durability.
- [x] 2. Admin permission checkbox: internal/admin/mdm_control.go, web.go,
  mdm_editor.js; distinct policy-save action with revision and existing CSRF.
  RED HTTP/UI contract then GREEN; preserve temporary editor behavior.
- [ ] 3. Android device editor: MdmUserEditorActivity/Session, packaged shared
  mdm_editor.js and mdm_phone.html; exact asset allowlist and two-op native bridge.
  Extend mobile/Kotlin operation allowlists; settings entry; immediate existing
  MDM sync after successful submit. No privileged tokens passed to JavaScript.
  RED transport/session/UI tests then GREEN, APK/lint and targeted Note9 tests.
- [ ] 4. End-to-end persistence/provisioning acceptance, full Go regressions/race,
  docs, one fresh reviewer and one RED→GREEN fix pass; local commits and archived evidence.

## Review Focus
- Stale phone draft after revoke/regrant or administrator write must not overwrite.
- Retry after lost response must not expose secrets or create multiple revisions.
- Imported documents and WebView navigation cannot escalate rights or bridge access.
- Phone pause/detach/rotation and quiet-mode sync must not apply stale local state.
- Admin and temporary web editors retain existing behavior and validation.

## Execution status
Tasks 3–4: code/build/automated Linux, JVM and Node checks completed; independent review
and one RED→GREEN fix pass completed. Note9 instrumentation installation was rejected by Android
`INSTALL_FAILED_USER_RESTRICTED`; physical acceptance remains unchecked.
See [evidence](../../testing/mdm-user-edit-2026-11-04.md).
