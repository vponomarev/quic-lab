# Android 0.9.5-pre.11

Published 2026-10-25 through the existing application update channel. Version code 76.

## Changes

- Automatic connection groups support AWG, VLESS and mixed transports. A VLESS subscription remains one logical connection while its nodes participate in selection.
- HA paths use the existing server multiplexor and fixed exit. A late LTE reserve can replace a redundant same-network spare without replacing the active primary; AWG peer exclusivity and budget policy remain enforced.
- VLESS availability map and per-network probe results; sequential Wi-Fi then LTE when LTE is permitted, LTE-only when already using LTE.

## Release validation

Linux verification uses an isolated source copy on the existing test host. Full Go suite and vet passed; the complete race suite passed with the existing narrow upstream Xray checkptr exception. Bundled quic-go race tests and all 60 installer tests passed. Corrected stale test expectations for WS/TLS, diagnostic receipt retention, exclusive AWG peer ownership, and installer packaging fixtures. The tcpdump integration test now observes unique packet markers with checked UDP I/O instead of a byte threshold; its initial intermittent failure was not conclusively reproduced.

Android native AAR rebuilt; APK, instrumentation APK and lint passed. The signing certificate matches published 0.9.5-pre.10. Note9 upgrade installation succeeded without removing application data. Android regression: 83 passed, one secondary-LTE test skipped because the saved reserve permission is disabled; full real HA validation on the final APK passed both 90-second Wi-Fi/LTE transitions with one TCP connection and the same session per leg. Configuration and radio settings were restored. Another 15 updater/profile tests passed.

APK SHA-256: `e8a7386ad3749eba871441db20854bea313ad0be318f9ef4a016450d242afca3`.

Publication updates only the APK and version metadata. Existing server binaries are retained. Main-service restart is required to reload the metadata; a rollback copy is prepared by the publisher before replacement. Note12 installation remains user-controlled through Updates.

Public metadata and independently downloaded APK hash verified. Server service active; server binary unchanged. Rollback: `/opt/quic-lab/rollback/android-pre11-20261025`.

The first qualified full race run hit the previously observed intermittent two-path-recovery timeout; three focused runs and the final complete run passed. No timeout was increased. Full evidence, corrected test assumptions and remaining validation qualifications: [verification report](testing/preview-release-gate-2026-10-25.md).
