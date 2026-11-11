# Unified GitHub preview 0.9.5-pre.15 — 2026-11-12

User explicitly authorized source completeness audit and publication of Android,
Linux client, Windows client and Linux server in a GitHub release. Preserve old
releases; publish new prerelease, source tag and current source branches. No
production/phone/laptop deployment is part of this request.

Build implementation base22da2e0. Android79/pre15; Windows installerpre15;
Linux clientamd64 nativeGUI; Linux serveramd64/arm64 allfourworkers.
Only tracked source is sent to authorized root@192.168.5.214; no field profiles,
keys, user data, build caches or private deployment backups enter release assets.
Use currentcheckout, preserve otherdirtyworktrees. GitHub connector has no
release/upload API; use authenticated gh CLI. User publication authorization
covers final verified uploads and normal fast-forward branch updates.

- [ ] Audit complete tracked sources and independent checkout build inputs.
- [ ] Build Android AAR/APK with certificate continuity and Gradle checks.
- [ ] Build self-contained Windows installer, verify signed Wintun and tests.
- [ ] Build Linux client package/licenses and both complete server archives.
- [ ] Verify archives, source provenance, checksums and relevant tests.
- [ ] Publish source and prerelease only after four components are present;
      verify remote asset digests and record final URL/evidence.
