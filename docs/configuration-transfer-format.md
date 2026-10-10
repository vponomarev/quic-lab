# Portable configuration transfer (development)

The implementation is not yet exposed in Android UI. Stable 0.9.2 remains the public download.

Envelope: `format=quic-lab-config`, `version=1`, `credentials=omitted|included`, `payload` with one or more `profiles`, `routing`, `network`, `diagnostics` sections. Sections not selected are absent. Profile IDs are explicitly mapped on import, never matched by display name. Add/replace allocate fresh IDs; update requires existing IDs selected by the caller. Full replacement requires every section. Imported profiles without credentials are reported separately and excluded from enabled profiles. The Android caller must not automatically start an incomplete current profile.

Profile settings exclude application selection and routes; those live in the routing section. Network includes DNS, configured LTE limit and reserve preferences, never used bytes or accounting epoch. Enrollment/update tokens, MDM binding and rights, diagnostic authentication and queues are not representable in schema1 and never transferred. Identity export is explicit; the caller must encrypt secret-bearing files and reject plaintext secret imports.

`Prepare` is pure: it produces a validated candidate plus selected sections and missing-credential IDs. It does not write files, mutate a phone or execute commands. Candidate can contain secrets; do not render or log it. A UI should render a redacted comparison and enforce generation checks before Apply.

Encryption: binary age passphrase format, scrypt factor16 on export and maximum18 on import. One KDF per process at a time. Plaintext maximum1MiB, ciphertext maximum2MiB. Decryption returns plaintext only after authenticated EOF; truncated and altered inputs fail without partial output.

Validation evidence: RED→GREEN for missing APIs, strict section presence and accidental identity inheritance on replacement. Linux configtransfer/mdm tests and configtransfer race/vet pass; mobile regression passes. Android encrypted store tests passed on Note9: 6 tests including local transaction, stale preview refusal and rollback retention. Build/lint passed. Android preview/apply orchestration and UI integration remain in progress.
