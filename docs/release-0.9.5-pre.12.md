# Android 0.9.5-pre.12

Version code 77. Android preview update, 2026-10-27.

## Changes

- Choose an allowed VPN profile and its provisioned QUIC, HTTPS or AWG transport while MDM stays active. Stop only the VPN before switching; radio-history collection continues.
- Delegated transport choice survives restart and ordinary sync. Explicit server changes and revoked permissions take precedence. General configuration editing remains protected.
- MDM pause now warns that collection will stop and pending measurements will be deleted; cancelling preserves management and the queue.
- Includes prior preview work: AWG/VLESS mixed automatic groups, Wi-Fi/LTE path probing and availability map, and LTE reserve preparation for HA via the common server exit.

## Validation

Android APK/instrumentation build, unit-test task and lint passed. Signing certificate is unchanged. Full Linux Go suite, go vet and 60 installer tests passed. Final versioned Note9 regression: 125 tests reported, with 124 passed and 1 skipped; no failures. Existing upstream Xray race/checkptr qualification and historical HA acceptance are documented in docs/testing/preview-release-gate-2026-10-25.md; no new server binary is deployed by this Android update.

APK SHA-256: `f09d25326d17340ad25fc7ea50ebb725380565c4ad704b0bb9ffb81813835551`.

APK and capability metadata are published through the existing update channel. Source changes are available in this release tag; private field profiles, keys, device logs and movement history are excluded.

Public capabilities and independently downloaded APK verified. Rollback: `/opt/quic-lab/rollback/android-pre12-20261027`. The server binary is unchanged.
