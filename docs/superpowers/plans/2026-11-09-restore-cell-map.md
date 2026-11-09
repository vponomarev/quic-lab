# Restore deployed cell map in server pre.14

> Execute inline using superpowers:executing-plans. User explicitly authorized fixing and publishing the server and Git updates. Existing worktree retained; do not alter the old dirty worktree.

Goal: restore the previously deployed cell-location map, persistent SQLite cache/queue/history and date preservation without regressing pre.13 server exits or MDM editing.
Specification: recovered 2026-10-27 cell-track and 2026-10-28 cell-database designs, plus 2026-11-01 date acceptance.
Base: 7734f63. Server release 0.9.5-pre.14; Android remains pre.13/code78.

- [ ] Reproduce missing authenticated map/assets/status in a release regression test on Linux.
- [ ] Port only related tracked diffs and allowlisted new source/vendor/docs from phase1-vpn; retain a hash manifest. Preserve current server-exit/MDM additions.
- [ ] Verify Go full suite/vet and cellgeo/admin/backup/MDM race, Node map/date/editor tests, synthetic browser fixture; one independent integration review/fix pass.
- [ ] Build both Linux archives from pinned source. Validate and back up existing production state, replace server binary with compatible rollback handling. Verify authenticated map/assets/database/read-only history without logging private data or initiating new rescans.
- [ ] Publish source tag and GitHub server prerelease with checksums; update user navigation and release evidence.

## Constraints / ledger
Use only source/synthetic fixtures on 192.168.5.214; Go tests never Windows. Production is existing vitaly@quic-lab.vpnc.ru. Preserve current data/config/APK and durable revocations. Backup stays on production. No automatic old-state restore after startup.
Root cause: previous production hash c116a407... matches documented cell-dates deployment; map source remained dirty/untracked outside release checkout. pre.13 build omitted it. Prior release checks did not cover authenticated map capability. New release regression must prevent recurrence.
Provider behavior remains existing Mylnikov adapter and persisted queue; no new provider or forced backfill. Existing authorization is recorded in recovered cell-database acceptance. UI must retain privacy/source wording.
