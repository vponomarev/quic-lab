# QUIC Lab v0.8.0-pre.1

Intermediate client/server prerelease at the A1–A3 and B1–B3 checkpoint, before group C. Android version code 21; Android 11+ ARM64/x86_64. APK uses the existing debug signing identity for manual sideloading and preserves installed profiles.

## Included

- Versioned configuration projection, independent exit lifecycle and route ownership. Pausing an exit blocks its assigned routes while other exits continue.
- Bounded bond sessions with a 120-second default disconnect grace and generation-safe path replacement. Session draining preserves existing sockets.
- QUIC and HTTPS WebSocket paths share an authenticated session and retain TCP sockets and UDP mappings across transport replacement.
- Payload-progress and working-size-probe telemetry, plus the portable profile-selection policy. Tiny liveness replies do not clear stalled payload state.
- Updated Android regression fixtures for the current session-count UI and explicit LTE network requests.

## Compatibility and limits

Upgrade client and server together: bond now uses QUIC ALPN `quic-lab-bond/2`; old bond handshakes are incompatible. Server restart disconnects active sessions. Ordinary QUIC/HTTPS/AWG modes remain available.

This is not the completed phase-one MVP. Executing the new automatic profile policy and carousel in Android is B4; shared LTE accounting is C1/C2. Automatic session rotation is intentionally rejected with a finite legacy LTE budget until accounting is shared. Existing flows remain alive. IPv4 only. HTTPS retains head-of-line blocking. Working-size probes test the transport path, not the final Internet exit.

## Downloads

- `quic-lab-0.8.0-pre.1.apk`
- `quic-lab-server-0.8.0-pre.1-linux-amd64.tar.gz`
- `quic-lab-server-0.8.0-pre.1-linux-arm64.tar.gz`
- `SHA256SUMS`

Server archives include AWG/transit workers, installers, documentation and licenses. ARM64 is cross-built; runtime checks use amd64. No production configuration, credentials, signing keys or diagnostic logs are included. Existing capture helper downloads from v0.7.4 remain separate.

## Validation

- Fresh Linux `go test ./... -count=1 -timeout=180s`, `go test -race ./... -count=1 -timeout=240s` and `go vet ./...`: pass. Live opt-in tests are recorded separately below; packages without tests are not coverage claims.
- Installer unit suite: 13 pass. Eight disposable container scenarios: Debian 12/13 and Ubuntu 22.04/24.04, each with direct and nginx frontends. Fresh install, repeated install, preserved credentials/configuration and rollback passed. systemctl is simulated in these containers; generated units are validated separately by systemd-analyze.
- Android AAR/APK, lint and test APK: pass. Core configuration/lifecycle/routing tests: 27 pass. Full standard suite: 35 passed, 16 explicitly gated tests skipped, zero failures (runner reports 51 tests). Separate live runs below enable relevant gated cases.
- Live phone MultipleLiveTest: pass, including real TCP/UDP, UID/CIDR priority, direct traffic, paused-route blocking, other-exit survival and ordinary QUIC/HTTPS regression.
- Live phone BondDeviceTest against the upgraded server: pass, one persistent TCP connection across Wi-Fi loss, LTE operation and Wi-Fi return, plus a short screen-off check.
- Live phone authenticated profile Echo on QUIC/HTTPS/AWG and transit: pass. Screen-on/off RTT and health policy verified separately on each of the three VPN transports: all three passed.
- Linux public QUIC/HTTPS/AWG Echo and transit test against the upgraded server: pass. Public APK download SHA256 matches the release artifact.
- SHA256 comparison confirmed all 613 Go/module/installer source files on the Linux build host match the publishing worktree.

Long-duration eight-hour soak, 15-device/100-Mbit acceptance and future B4/C/D/E functionality have not been certified by this checkpoint. Optional tests requiring separately provisioned capture identities or failure orchestration are not counted as passed merely because the standard runner skips them.
