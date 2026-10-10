# Temporary WEB editor — development checkpoint 2026-10-10

Development APK: 0.9.4-pre.1 (57). Public release remains 0.9.3. Production has not been updated with the editor.

## Implemented

- Administrator entry: `/lab/editor` (also available on a management-only server). One-time QR/link, five-minute pairing window, session durations 15/30/60 minutes, default 30. The phone must explicitly approve. Credentials travel in the link fragment and POST bodies, never URL queries.
- Independent RAM-only device session over physically bound HTTPS with TLS validation, no redirects, shared LTE accounting and cancellation. No persistent enrollment or automatic VPN start. Process restart cannot restore the temporary credential.
- Android Settings → Temporary WEB editor: QR/text entry, server/scope/duration disclosure, explicit approval, remaining time, local Stop and foreground notification. Expiry uses elapsedRealtime and includes device sleep.
- Shared browser configuration/application form; explicit Apply; generation comparison; pending/applied/error state. Phone local edits invalidate a queued older candidate. Existing keys stay on the phone unless explicitly replaced. Unused incomplete profiles are allowed and excluded from enabled outputs.
- Application inventory is bounded and subject to Android package visibility. Selected missing packages remain visible. Application labels are rendered as text.
- Browser import/export uses the portable format and existing Go validator/merger. Sections can be selected; import supports add, explicit mapping/update, and replace confirmation. Import changes a draft; Apply is separate. Browser export omits credentials; encrypted key export remains on the phone.
- Browser owner/CSRF checks, separate pairing/browser/device credentials, terminal expiry recovery in UI, logout revoke, and scheduled server payload deletion even with no subsequent requests.
- Backup polling moved to same-origin external JS to comply with the existing CSP.

## Authority ruling and remaining scope

Active MDM configuration ownership is not delegated to a temporary editor token. Such phones are directed to the existing authenticated MDM device editor, whose desired/applied flow and binding epoch checks remain authoritative. A new temporary delegation tied to the same MDM binding/epoch is **not implemented**. No pause or consent was changed to make tests pass.

The core local editor works end-to-end; this checkpoint is not a stable-release acceptance claim. Still pending:

- Dedicated persisted editor lifecycle audit with the intended retention policy.
- Full user-driven consent/QR UI acceptance and temporary-session OS process-death/reboot acceptance on Android 12/16; only Android 11 was connected in this turn.
- Live-traffic configuration change, manual Stop race, and two-browser/newer-local-edit end-to-end acceptance beyond store/isolated runtime tests.
- Full browser import/export interaction acceptance (routes/format validation tested; real HTTPS editor Apply tested).
- Full backup-job browser acceptance after the CSP extraction.

## Evidence

- Linux `go test ./...`: PASS (mobile 53.146 s; other packages include cached unaffected results).
- Linux editor/admin race suite: PASS; editor/admin vet PASS. Expiry-without-traffic regression passes under race.
- Chromium/Playwright shared form fixture: PASS pairing, disabled Apply before consent, editing, pending ACK, browser revoke, recovery after phone revoke/expiry. Test: `scripts/test-temporary-editor.py`.
- Note9 / Android 11: 15 tests across TemporaryEditorTest, ConfigurationTransferTest and MdmConfigurationStoreTest PASS (8.331 s). Another 15 MDM configuration/apply and LTE persistence tests PASS (9.878 s).
- Real HTTPS fixture on 192.168.5.214:18449: browser creates session → isolated Android runtime pairs → browser changes detailed diagnostics → phone atomically applies → browser receives applied ACK → phone revokes → browser enables a new session. Android live test PASS (19.026 s); Chromium PASS. Test used synthetic VLESS credentials, isolated files/preferences and a fake VPN port; no working phone configuration or MDM consent changed. Custom CA was supplied only by the instrumentation fixture. Temporary server stopped after the test.
- Gradle assembleDebug, assembleDebugAndroidTest and lintDebug PASS.
- Independent review found three issues (stuck expired browser session; unused incomplete profiles blocking edits; payloads retained past expiry without requests). All three reproduced with regression tests and corrected.

The live fixture does not prove camera scanning, consent-screen interaction, continuous VPN traffic or process-death behavior. Those remain explicit acceptance work.
