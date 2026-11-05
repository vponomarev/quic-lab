# Android/server 0.9.5-pre.13 release plan

> Execute inline with superpowers:executing-plans. User explicitly authorized an intermediate release and updates of Note9 and the existing server; no additional publication approval is needed.

**Goal:** publish a matching Android/server preview and upgrade Note9 and quic-lab.vpnc.ru with rollback evidence.
**Architecture:** existing preview APK signing/update channel, tagged source, Linux archives containing all four server binaries. Preserve profiles, keys, MDM and network configuration.
**Stack:** Gradle/gomobile, Go/Linux, Python deployment, SSH/ADB, GitHub releases.
**Specification:** current request; existing docs/install-server.md, docs/server-exits.md, docs/testing/mdm-import-2026-11-05.md.

## Constraints
- Reuse clean codex/server-exits worktree at 5a8ec89; preserve other dirty worktrees.
- Go tests only on authorized Linux test host 192.168.5.214; transfer tracked source only.
- Production is vitaly@quic-lab.vpnc.ru; existing key/host verification and sudo, no secrets in logs or release assets.
- Update server, AWG, transit, VLESS together; stop old workers before starting new ones. Preserve existing legacy exit policy.
- Back up binaries/configuration/persistent data on the production host before replacement. Roll back on failed health checks.
- Version 0.9.5-pre.13, Android code 78; match previously published signing certificate and upgrade without data removal.
- Publish preview only; retain documented physical acceptance limits. Windows binaries are outside this release.

## Tasks
- [x] Pin source/version and build native AAR/APK plus both Linux archives; check signatures, hashes and package contents.
- [x] Run Linux suite/vet/relevant race/installer checks and Android build/lint/device regressions; one release-specific review of packaging/rollback.
- [x] Back up production, preflight current config with new binary, replace all workers and publish APK/metadata; verify services and public download. Upgrade Note9 and verify installed version.
- [ ] Publish matching source tag and GitHub prerelease with APK, archives and checksums; record deployment evidence and remaining limits.

## Ledger
Ruling: release/deployment authorized by current user request; no extra confirmation menu. Production identity recovered from prior publisher and verified via SSH. Current server and phone metadata: pre.12/code77. Production data about 986 MiB, free disk 4.9 GiB.
Ruling: connector lacks release/asset-upload tools; use authenticated gh CLI for the existing repository release workflow.
Review focus: exact source provenance; certificate continuity; consistent workers; private-state backup and rollback; metadata/download hash agreement.
Review: four updater Important issues fixed before deployment (durable-state preservation, temporary cleanup, partial-stop recovery, VLESS readiness). Four failure-injection tests passed; no rereview. Build/version f2bb0f9, final 98/98 Note9 regressions plus real AWG/transit echo, Linux suite/vet/race and 60 installer tests passed. Production and Note9 now pre.13; public download hash verified. [Evidence](../../testing/preview-release-2026-11-05.md).
