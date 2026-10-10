# MDM stable 0.9.1 acceptance

Date: 2026-10-10. Completes the current MDM iteration after VPN phase 1; Remote Helper, temporary editor sharing, group configuration and research mode remain later work.

## Scope

- Independent MDM enrollment, explicit permissions, pause/resume/delete; current and external device configurations.
- Web configuration and application selection, optimistic concurrency, command results and device reports.
- Wi-Fi/cell telemetry, separately consented coordinates, local LTE prohibition and durable queued delivery.
- Standalone management-only server/installer without VPN/Echo listeners or provisioning routes; existing combined installs unchanged.
- Encrypted GUI/CLI config/full backups, offline verified restore/rollback and CLI automation interface.

## Evidence

- Linux: complete `go test ./...`; race and vet for internal/mdm, internal/admin, cmd/server and mobile PASS. Installer 60 tests PASS.
- Management-only process starts with the configured Echo UDP socket already occupied, exposes MDM and rejects VPN/capture endpoints. Conflicting listener/worker configuration is rejected.
- Release archive: CLI config/full, GUI download, verification, live-owner restore rejection, offline restore/start and rollback PASS.
- Android: 0.9.1 code51 builds and lint pass. Note12Pro API31: 44 tests passed, one real-HTTPS case initially skipped without fixture; that case then ran separately against the Linux HTTPS fixture and passed (report, remote apply, acknowledgment, pause/delete). Original production MDM binding and rights preserved.
- Fresh independent review found no release blockers. Standalone invitation navigation corrected.
- Pre-deployment encrypted full backup verified and copied off-server (2244 manifest entries); password remains in local Windows-protected private storage.

## Explicit remaining validation

- Android14/15 foreground-service runtime validation is deferred by user until a suitable phone is connected. Evidence here covers Android11 historical acceptance and Android12 current acceptance only.
- Browser native confirmation stalls the current CUA/CDP tool; two-editor browser end-to-end and responsive acceptance are not marked PASS. Server CAS/conflict tests pass.
- Earlier system-DNS hostname probe did not finish and remains a VPN follow-up; tunnel DNS traffic passed earlier acceptance. No claim of a diagnosed DNS defect.
- Full physical-network/failure/load/8h VPN matrix belongs to phase2 under the accepted v0.9.0 scope decision.
- Existing backup page polling uses inline script under restrictive CSP, so a manual refresh may be needed; archive operations are tested independently.

## Deployment and final entry checks

- Deployed 0.9.1 on quic-lab.vpnc.ru; server, AWG, VLESS and transit services active. All three MDM bindings preserved; fresh device reports received.
- Public capabilities advertise 0.9.1/code51. Downloaded APK SHA256 matches b8aed1a32f0acbdf18193bdeda033daf08dd2212dbb29b8ddc549c8fa0dd8348.
- Note12Pro reports 0.9.1 and VPN running; remote start acknowledged. Actual QUIC/HTTPS TCP exit and UDP DNS tests passed five times each after deployment.
- Direct MainActivity entry previously bypassed launcher routing and showed the old lab screen. Observed RED, split Echo into an unexported diagnostic activity, retained MainActivity as unconditional VPN router. Final unlocked Note12Pro direct-entry and root-navigation tests: 2 PASS/5.293s. Build and lint PASS. Late bounded review found no code blocker.
- Fresh process-death simulation via run-as kill was rejected by Android; the process remained alive. This is NOT a successful process-recovery test. Reboot/process-death acceptance remains pending on the dedicated Redmi, which currently reports USB unauthorized. Existing isolated lifecycle/recovery tests passed as listed above.
- Server rollback files retained in /root/quic-stable-091-upgrade-20261010/previous. Pre-upgrade encrypted backup is also stored off-server.
