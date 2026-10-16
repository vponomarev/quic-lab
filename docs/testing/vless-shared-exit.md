# VLESS shared exit: development verification

Date: 2026-10-17. Implementation is in progress; no Android release or production activation yet.

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

Existing bond tests also cover pending limits, retention, ACK/deduplication and datagram expiry. Device LTE accounting, Android TUN continuity, MDM reference validation/editor and release acceptance remain outstanding. Note12 was left stopped and its active configuration was not changed.
