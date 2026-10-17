# VLESS shared exit: development verification

Date: 2026-10-17. Android/MDM integration is implemented for 0.9.5-pre.5; Note12 activation remains separate from publication.

## Verified transport core

- Authenticated HTTPS bond carrier runs through an independent VLESS engine. Capability requests use the same underlay.
- Wrong internal TLS identity, redirects, invalid VLESS configuration and canceled handshakes are rejected; path cleanup is idempotent.
- Relay runtime admits two distinct candidates, replaces a failed candidate, bounds retries, cancels pending dial on Stop and rejects direct fallback/mixed gateway identities.
- Physical VLESS endpoint resolutions belong to a Network generation; absent resolution cannot reuse another network's address.

`bash scripts/test-vless-shared-exit.sh` runs two real TLS VLESS proxies, an mTLS gateway and a TCP echo destination in a disposable Linux network namespace. Only loopback exists inside it; host routes are untouched. Output is test2json JSONL.

The fixture transfers sequenced 128-KiB payloads, disables the primary carrier, disables both carriers for 10 seconds while data is pending, then joins a replacement to the same session. It asserts one destination accept, identical mux/token and exact payload. The blackhole variant silently discards carrier writes after readiness, rather than closing the connection.

Observed namespace run: reset failover 29 ms; blackhole failover 78 ms. Both retained the original destination socket and delivered pending data after the outage. These are local fixture measurements, not mobile-network SLAs or public exit-IP measurements.

The blackhole regression found and fixed a scheduler defect: among carriers on one physical network, stale historical RTT could outrank a healthy reserve. Fresh, unpenalized paths now rank ahead of stale/penalized paths within that network. This does not establish the cause of the separate standalone VLESS incident on Note12.

Full Linux verification passed:

```sh
go test -race ./mobile ./internal/bond ./internal/bondhttps ./internal/vpnmodel ./internal/pathpolicy -count=1
```

Existing bond tests also cover pending limits, retention, ACK/deduplication and datagram expiry. Physical LTE accounting is covered by the shared-budget tests; actual blocked-network LTE handover remains field acceptance. Note12 configuration was not changed during development.

## Android acceptance checkpoint

Note9 (`dc69eb2c`), actual Android TUN and separate probe UID, two REALITY VLESS servers on the Linux lab host and an inner mTLS gateway bound only to server loopback. The target accepted exactly one TCP connection; after stopping the first VLESS server, 387 bidirectional exchanges completed with exact payload and maximum exchange RTT 40 ms. Logical bond connection ID stayed unchanged. This proves the tested Wi-Fi carrier failure, not arbitrary mobile-network reliability or indefinite session retention. The test restores/deletes only its temporary profiles.

Fixture preparation problems were corrected separately: explicit Stop is required for a bound Android VpnService; fixture control uses pinned TLS; the gateway allowlist includes only loopback and the lab echo host. These were test harness defects, not product tunnel regressions.

The Android fixture is opt-in with `QUIC_LAB_RELAY_ANDROID_FIXTURE=/absolute/private/directory go test ./mobile -run TestRelayAndroidFixture -v -count=1 -timeout=17m`. It writes short-lived credentials to `fixture.json` mode 0600. Copy to the test app's private files as `relay-fixture.json`, run `VlessRelayDeviceTest`, then stop the fixture by creating its directory's `stop` file. Never publish the credential file.

Configuration tests cover invalid references, capability downgrade, incompatible pending identity merge, redacted public snapshots, stable candidate IDs, underlay-only DNS and retained addresses after late DNS. Final review identified reserve-policy and session-status regressions and absent TLS hostname fallback. All three were reproduced by regressions and fixed. Final Linux race tests passed; Android build/lint and 31 instrumentation checks passed. No deferred review findings.

## Final pre.5 acceptance and deployment

Final Note9 TCP+UDP instrumentation passed: 389 bidirectional TCP exchanges, maximum exchange RTT 41 ms, exactly one destination accept and unchanged logical connection after stopping a VLESS carrier. UDP echo through the surviving path also passed. This is measured exchange RTT, not a guaranteed failover SLA. Original profile restored; VPN foreground service restarted after testing. MDM remains in its original paused state.

Final Linux race command covered `./mobile ./internal/mdm ./internal/admin ./internal/bond ./internal/bondhttps ./internal/vpnmodel ./internal/pathpolicy`; all passed. Android build/lint and 31 configuration/status instrumentation tests passed, followed by the actual TUN device test. Production binary/APK deployed as 0.9.5-pre.5; public APK hash matches release notes. Note12 settings unchanged.
