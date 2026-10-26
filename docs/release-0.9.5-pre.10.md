# Android 0.9.5-pre.10

Published 2026-10-19 as a preview update, superseding 0.9.5-pre.9.

Includes the all-source VLESS probe, with TCP and WebSocket/TLS transport support, IPv6 endpoints and up to 256 nodes in one run. Subscription profile identity now remains stable across provider UUID rotation. Probe fingerprints still include the complete candidate configuration, so changed credentials invalidate earlier results.

Validation: `go test -mod=readonly ./internal/vless -count=1`; Android assemble, lint and instrumentation sources passed. On Redmi Note 9 Pro / Android 11, all 17 tests in `VlessSubscriptionsTest` and `VlessProbeStoreTest` passed, including UUID-rotation identity and credential-sensitive probe fingerprint checks.

Version code: 75
Version name: 0.9.5-pre.10
APK SHA-256: `ec93babffdb273c7a234d11179310f5486384f877cb15c2d359d9c7341774523`
