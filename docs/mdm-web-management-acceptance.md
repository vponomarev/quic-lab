# MDM WEB management — acceptance in progress

Date: 2026-10-08. Branch: codex/mdm-core. Plan: docs/superpowers/plans/2026-10-08-mdm-web-management.md.

Implemented: optional VPN user ownership of MDM bindings/invitations; user-card links; direct Android QR/text enrollment; consented configuration/application inventory reports; guarded config revisions; durable Android apply/cleanup and VPN commands; WEB configuration/app editor, preview and explicit Apply.

## Verified

- Linux 192.168.5.214, isolated /srv/quic-mdm-web: `go test -race ./internal/mdm ./internal/admin ./mobile -count=1` PASS (1.801s, 12.635s, 53.178s); matching `go vet` PASS.
- Subsequent report-only list regression RED then admin/mdm race PASS (12.614s, 1.735s).
- Android debug APK/test APK/lint passed. Native AAR rebuilt with report endpoint and reserve schema.
- Redmi Note 9 Pro dc69eb2c, Android API30: lifecycle/apply/runtime/config/store 26 tests PASS, 53.526s after cleanup deadlock fix. The deadlock regression failed before the fix (16.465s).
- Real HTTPS wire test against isolated Linux fixture: enrollment in isolated app storage, consented report, desired settings, atomic apply and revision acknowledgement PASS (12.014s). Real phone settings/consents were not modified by this test.
- Browser: MDM count in user list, MDM card tab, device editor, escaped application labels, selection diff, explicit Apply and pending revision verified.
- Fresh review resolved durable config-right cleanup, separate VPN restart failure, expiry after main/apply wait, and main/controller lock inversion.

## Pending release gates

- Live command/VPN acceptance with actual phone consent (isolated command/audit regressions already pass).
- Real bound phone consent, remote start/stop and configuration changes with real tunnel/direct traffic.
- Responsive/full-HD and two-editor browser acceptance; one browser session became unresponsive during second-editor test. Server CAS/idempotency tests passed separately.
- Public APK publication and download verification after live phone acceptance. Candidate 0.9.1-pre.9 (49) built/linted and installed on Redmi.
- Note12Pro / Android API34+ acceptance: phone not connected in this session. Do not treat API30 tests as evidence for all Android versions.

## Latest verification and staged deployment

- Additional Android regression: 57 reported tests PASS / 44.032s (MDM commands/lifecycle/report/telemetry/budget, VPN settings/profiles/budget, updates). Opt-in physical recovery scenarios were not enabled; the reported count is not proof of those hardware scenarios.
- Command durable deduplication and elapsed TTL tests pass. Repeated config failure audit regression RED (3 events instead of 1), then GREEN after durable result deduplication. Failed config retry is throttled to 15 seconds.
- Final Linux source archive SHA256: ec2c36938d7ee9457a1030a7367310de4776f19ee6ccfe46864186f8f10511fd. Same digest verified after upload. Full affected race suites PASS (mdm1.800s/admin12.639s/mobile53.272s), vet PASS, server build PASS.
- Server binary SHA256: b9c03bdb4cd027ca261a43b98bf7a2e47d7c60cafb5c4e7310914a0f180f747f. Deployed to quic-demo.vpnc.ru; active service and public editor script verified.
- Backup: /var/backups/quic-lab/mdm-web-20261008-200054 (binary/configs/quiesced MDM state). APK and public version remain pre.8. Old server loads the expanded JSON but can discard new fields on write: preserve the new state before any downgrade; do not silently restore a stale MDM state over new registrations.
- Candidate pre.9 build/lint PASS / 40s; installed on dedicated Redmi. Read-only probe confirms no MDM binding/consents. Invitation screen opened, awaiting explicit user confirmation; no permissions auto-granted.

Ruling: stage the backward-compatible server before live phone acceptance so production enrollment/report/control APIs are available. Client publication remains gated on actual remote VPN/config acceptance. Private invitation is excluded from Git.


## Live acceptance and ANR correction (2026-10-08, final session)

This section supersedes the earlier pending-consent and unpublished-APK status.

- User explicitly enabled MDM rights on the dedicated Redmi. Remote VPN start and stop, reports while VPN was off, and current-mode configuration revisions were exercised against production. Inventory contains 494 application entries.
- Revision 1 changed profile label, selected applications, DNS mode and budget; revision 2 restored the original configuration. Raw UDP DNS and HTTPS to an IP succeeded through the selected VPN probe application; the separately installed direct probe completed HTTPS. The system-DNS hostname probe did not complete before being stopped: do not count that case as a successful DNS acceptance test. The original tunnel-DNS configuration passed hostname HTTPS.
- The user reported an ANR at 23:29:06 MSK. Official Android bugreport confirms main waiting for MdmConfiguration.lock from LabVpnService.onStartCommand; mdm-control held that lock while repeatedly loading AndroidKeyStore during configuration snapshot. Sensitive raw bugreport remains local/ignored.
- Added a bounded process-local decoded-document cache. Every load still reads and compares complete ciphertext; changed bytes require authenticated decryption. Returned JSON is detached. No plaintext is written to disk. Cache lifetime is process-local; it does not replace durable configuration state.
- Regression RED: 100 reads took 3241 ms (limit 1000 ms). GREEN: 167 ms, all 4 store tests pass, including same-length ciphertext corruption, caller mutation and cross-instance changes. Build and lint PASS (44s). Additional lifecycle/apply/runtime/config/store/report/command regression: 31 tests PASS (26.663s). Focused independent review found no blockers.
- Fixed candidate installed on Redmi. Remote start and revision 3 (original configuration) succeeded; server acknowledged revision and running VPN. Original observed configuration equals the backup exactly. HTTPS example.com probe returned vpn=true/ok=true. Normal launcher opened VpnActivity in 246 ms. Last-ANR timestamp remained the original 23:29:06 throughout these checks; this is a bounded reproduction check, not a long-duration soak.
- Published 0.9.1-pre.9 (49). Public capabilities and downloaded APK verified. SHA256: f60460f39bc601ee15e958e7ca013b1b3fe0e2b0f5daed8070acfd7753f5c673. Server service restarted for version metadata; MDM report after restart confirms original settings and VPN running.
- Remaining acceptance: two-editor/responsive browser checks (CDP stalled around native confirm), system-DNS hostname probe investigation, and Note12Pro/API34+ live acceptance. No claim of full phase completion.
