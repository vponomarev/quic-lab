# Android/server pre.13 acceptance — 2026-11-05

Source implementation base `5a8ec89`, build/version commit `f2bb0f9`.
Release tag `v0.9.5-pre.13` adds documentation only after the build commit.
Tracked-source archive SHA-256: `7c8527a9e24a7d9bee61e2c659edd61dfdc47122a15a29afb71fc493181758b2`.
Go tests ran only on the authorized Linux host in `/tmp/quic-release-pre13-20261105`.
No field profiles, keys or device data entered source archives or release assets.

Published [GitHub prerelease v0.9.5-pre.13](https://github.com/vponomarev/quic-lab/releases/tag/v0.9.5-pre.13),
tag commit `0bad999`. APK, both server archives and SHA256SUMS.txt are uploaded;
GitHub-reported SHA-256 digests for all four assets match the local files. Only
the tag was pushed; existing shared branches were not merged or rewritten.

## Validation

- `go test ./... -count=1`, `go vet ./...`: PASS.
- Race: MDM, serverexit, exitcontrol, configtransfer, admin: PASS.
- Installer Python suite: 60/60 PASS. Node phone editor/probe tests: PASS.
- Native AAR rebuilt from the pinned source, arm64 and x86_64. APK/instrumentation builds, Android lint and JVM 2/2: PASS.
- Final versioned Note9: 98/98 regressions PASS, 53.510 s, no skips. Covers MDM editor/import, cancellation/lifecycle, quiet mode, selection/apply, groups, remote probes, updates, service transfer and server exit selection.
- Post-deployment Note9 `PublicEchoTest#anonymousAwgSession`: 1/1 PASS, 0.663 s; real AWG local and transit echo, no system VPN/profile replacement.
- Installed app: `ru.vpnc.quiclab`, code 78, version `0.9.5-pre.13`; installed with `adb install -r -t`, no data wipe.
- Certificate SHA-256 unchanged from published pre.12: `1b815915bdc41b179ecbe5d106e06a2ca83205f573a964979dd7af4e357db68c`.

## Artifacts

| Artifact | SHA-256 |
| --- | --- |
| quic-lab-0.9.5-pre.13.apk | `0844fcc490289387e4ec836f3f79c808b9538230c701d070a9e1413e77e76bdf` |
| quic-lab-server-0.9.5-pre.13-linux-amd64.tar.gz | `eddd8797d6cd9fcaffc46b8ba6b7da58389e3d6c4bbeaa96b2e8bb6ea20348cd` |
| quic-lab-server-0.9.5-pre.13-linux-arm64.tar.gz | `367e2605d4264bbf5d1c5cc1e98305273ba35024f77720a07dfbe4cb2f5b2797` |

The existing preview APK/update channel is retained. Public metadata reports
code 78/pre.13; an independent HTTPS download of 60,710,969 bytes matched the APK
hash. Both server archives contain all four corresponding binaries.

## Deployment and recovery

Production `quic-lab.vpnc.ru` updated after current-config validation and guarded
hash checks. All four old workers stopped before replacement. Configuration,
legacy exit mode and identities were retained; only APK metadata was modified.
Server, AWG, transit and VLESS services are active, NRestarts=0. Running executable
hashes match staged binaries. VLESS confirmed the authoritative applied revision.

Private backup `/opt/quic-lab/rollback/pre13-20261105` contains previous binaries,
configuration, APK and a consistent stopped-state archive (1,025,464,320 bytes).
It remains on the server; it is not a GitHub asset.

One release-specific review found four Important issues in the one-shot updater;
all addressed before production changes:

1. Never automatically restore stale durable state after starting new services.
   Preserve revocations/MDM writes and install persistent recovery gates on all
   four units if a post-start failure requires compatible-state investigation.
2. Unique temporary replacement files with unconditional cleanup allow rollback
   after partial copy/fsync/ownership failures.
3. Record stop attempt before multi-service stop, so partial stop failures recover.
4. Check VLESS applied revision rather than process liveness alone.

Four synthetic Linux failure-injection tests against the actual transaction/
helpers passed: partial stop, partial copy, post-start acknowledged-write
preservation/recovery gates, and rejection of unapplied VLESS configuration.
No production rollback was needed; recovery marker absent after deployment.

Local raw evidence: `artifacts/release-pre13/`. Deployment scripts and ephemeral
logs are not public assets. No feature-code changes were required by release review.

## Limits

This preview does not claim full physical HA/Doze/OEM/battery acceptance, all
provider/protocol combinations or Windows/Linux desktop release qualification.
Prior isolated MDM HTTPS round trip and named-exit peer tests remain documented
in their acceptance reports. The production upgrade retained legacy routing;
new named exits require deliberate administrator configuration.
