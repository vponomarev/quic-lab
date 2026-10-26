# Android preview release verification, 2026-10-25

Work in the existing phase1-vpn worktree; preserve unrelated local work.

1. Correct obsolete Windows VLESS WS rejection, retain unsupported transport coverage; run windowsengine tests.
2. Transfer a source-only snapshot (tracked and non-ignored source) to a new directory on the documented Linux test host. Run full Go tests, vet, race tests and bundled quic-go race tests. Diagnose and fix actual failures before proceeding.
3. Verify current OTA metadata and signing compatibility; increment Android version. Build native library/APK, run lint and applicable Android regression tests on Note9 while preserving its current configuration.
4. Review final changes and verification evidence. Publish the APK and update metadata using the existing release mechanism only after all required checks pass. Verify the served APK hash and version for Note12.

Publication is explicitly authorized by the user, conditional on successful verification. No direct installation on Note12.

Completed: test corrections reviewed; full Linux suite/vet and qualified race passed; final Android build and Note9 HA passed; pre.11/76 published, public download hash verified. See docs/testing/preview-release-gate-2026-10-25.md for evidence and explicit qualifications.
