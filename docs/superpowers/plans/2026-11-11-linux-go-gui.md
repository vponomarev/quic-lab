# Linux Go GUI implementation plan

> Execute with superpowers:subagent-driven-development; preserve current worktree.

**Goal:** Migrate the installed Python GUI/controller to one Go/Fyne executable.
**Spec:** ../specs/2026-11-11-linux-go-gui.md
**Architecture:** Existing VPN engine reusable as internal/linuxcli; authenticated
Unix-socket controller in internal/desktopcontrol; Fyne UI in internal/desktopui;
installer in internal/desktopinstall; cmd/quic-lab selects modes. Build GUI only
for Linux, keeping other binaries independent of graphics dependencies.
**Stack:** Go, Fyne v2, systemd/journalctl, current socket/profile format.

- [x] Task 1: Port controller/protocol/parser to Go, security and rollback tests.
- [x] Task 2: Port GUI/state reducer to Fyne, controls/RTT/pending-action tests.
- [x] Task 3: Extract reusable Linux CLI; single entry and safe installer/migration.
- [x] Task 4: Build with compatible graphics toolchain, integrate and review; test
  runtime without display and existing CLI behavior. Fix evidenced regressions.
- [x] Task 5: Back up/migrate laptop, test all transports and actual GUI, leave
  QUIC active/autostart. Update docs/package and commit reviewed implementation.

Dependencies: Tasks 1/2 use the spec's explicit DTO/API; Task 3 consumes both.
Only root agent edits go.mod/go.sum and installer/entrypoint. No source containing
profiles or user data is sent to the authorized Linux test host.

Completion evidence: ../../testing/linux-go-gui-2026-11-11.md. Also ported the
recognized laptop DNS/IPv6 helper to eliminate the remaining Python runtime
dependency; its behavior and the service unit are preserved. Final laptop
acceptance passed all four profiles and controls; QUIC active and boot-enabled.
