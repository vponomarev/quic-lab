# Configuration transfer acceptance (development)

2026-10-10, Android 0.9.3-pre.1/code55. Public download remains 0.9.2.

Implemented: Settings entry, standard file picker, selectable export sections/profiles, optional password-encrypted credentials, add/update/replace with explicit mapping, redacted preview, Apply, last-import rollback. Import checks MDM ownership and configuration generation; persistent local operation supports recovery. Existing profile metadata is retained for explicit matches. Secret-bearing plaintext is rejected. Missing installed applications are shown and must be resolved before Apply.

Evidence: Linux configtransfer/mdm race and vet passed; full mobile regression passed. Android build/lint passed. Note9 Android11: 22 tests passed (5.371s), covering configuration transfer, isolated encrypted store, MDM apply, budget persistence and process recovery. RED→GREEN confirmed persistent disable-resume after meter release. Fresh review identified this bug, an exhausted mapping callback crash and missing application handling; all were corrected. Additional VLESS encrypted export regression passed: the VPN identity survives encrypted roundtrip, while update_token is excluded; all 6 transfer tests passed1.167s. On Note9 the Settings entry and transfer screen opened, export reached the standard Save picker and import reached the Open picker; both were canceled without modifying personal profiles.

Limitations/pending: full two-phone secret transfer and live UI file-picker/rollback acceptance; active-VPN import crash matrix; review of temporary editor not started. Temporary editor broker/UI (plan Tasks3–4) and backup CSP follow-up remain later work. Do not label the whole iteration complete or publish a stable release from these checks alone.
