# Verification: Android subscriptions and Linux preview

2026-10-16. Android 0.9.5-pre.1 (66), debug APK installed on Note9 dc69eb2c. assembleDebug, assembleDebugAndroidTest, lintDebug passed. Device instrumentation: VlessSubscriptionsTest 6 + MdmConfigurationStoreTest 5 = 11 passed. Mixed unsupported/VLESS list regression observed RED then GREEN. Encrypted subscription metadata, UUID rotation, local settings preservation and failed transaction checked. UI subscriptions screen opened. Existing AmneziaWG profile retained, VPN connected after tests.

Android: Connections → VLESS subscriptions → Add → HTTPS URL → preview → Apply. Manual refresh. TCP TLS/REALITY supported; unsupported entries skipped, ambiguous duplicate endpoint identity rejected, at most 32 total profiles. Removed remote profiles retained. Actual public provider URL download and a provider VLESS connection have not been exercised in this iteration; parser, persistence, UI and existing VPN preservation were tested.

Linux test host root@192.168.5.214, source /root/quic-linux-client-20261016. go test -race ./internal/linuxclient and go vet ./internal/linuxclient ./cmd/linux-vpn-client passed. scripts/test-linux-client.sh passed: real production TUN client, QUIC + HTTPS, TCP/UDP payload equality after 32 seconds, SIGTERM cleanup, server termination → client exit 1 → TUN cleanup. Host SSH route unchanged. TUN has no assigned IPv4 address. Two test namespace and fixture keys removed by cleanup. No persistent systemd service installed.

First Linux version: CLI, selected IPv4 subnets only, QUIC/HTTPS. See linux-client-preview.md for explicit limits. systemd template supplied but its installation/restart behavior was not live-tested. External host routing, full tunnel and physical interface handover not tested/supported here.

Independent code review found three issues (mixed subscription prefix, fixed TUN IP, transport-loss cleanup), all corrected; follow-up review found no further important issues. Artifacts: quic-lab-0.9.5-pre.1.apk; quic-lab-linux-amd64-preview.tar.gz; subscriptions-linux-SHA256SUMS.txt in artifacts/. No production server update or stable release published.

## Real provider verification (Note9)

User-provided private subscription tested in Android UI and temporary on-device instrumentation; URL and contents not retained in repository. Device fetched HTTP 200, ~126 KB, Xray JSON array of 41 configurations. Current text/Base64 VLESS subscription parser rejects this format. UI fails earlier because active MDM configuration authority forbids local edits (local_edit_allowed=false). No profiles or rights changed; no provider VPN connection attempted. Parsing/selection support for JSON arrays remains required; the existing 32-profile limit also requires deliberate selection rather than importing this whole set. Temporary probe removed after the test.

## JSON subscription follow-up: 0.9.5-pre.2 (67)

Implemented Xray JSON arrays, independent supported outbounds, canonical URI storage, selection dialog and remembered choices. Duplicate keys checked before normalization. Provider rotates SNI and REALITY shortId: stable identity now endpoint/security/flow. Endpoint changes remain new records; conflicting same-endpoint credentials are rejected.

Note9: 14 regression instrumentation tests passed, assembleDebug/assembleDebugAndroidTest/lintDebug passed. Opt-in VlessSubscriptionLiveTest passed: HTTPS 200 through first available supported VLESS endpoint, real encrypted configuration application, re-fetch/application without duplicate, original current profile preserved. Provider produced 25 unique supported entries in the final UI run; unsupported count varied 15–16 between responses. UI refresh showed remembered checkbox, preview added0/updated1, and successful apply. Full provider routing/balancing is intentionally not imported; no claim of checking every endpoint or full device TCP/UDP through every provider server.

User authorized pausing MDM. Original AmneziaWG was running again at end (UI: connected, Wi-Fi). MDM resume failed twice with generic MDM HTTPS exchange error; it remains paused, binding/rights retained. This is not counted as a passing restoration check. APK in artifacts/quic-lab-0.9.5-pre.2.apk; not deployed to production update feed. Private subscription URL and payload not stored in repository.
