# VLESS subscription probing verification — 2026-10-17

## Coverage

- Linux: `go test -race ./mobile ./internal/mdm ./internal/admin ./internal/probeecho ./internal/bond ./internal/bondhttps ./cmd/server -count=1` passed. MDM/admin rerun after epoch-interruption fix passed.
- Transport: real VLESS carrier, exact nonce echo, redirect/wrong nonce/oversize/untrusted TLS rejection, cancellation, separate authenticated relay, certificate-name mismatch distinguished from exchange failure.
- Android: bounded two-worker engine, delayed cancellation and worker drain, interrupted-run storage, MDM capability parsing, owner binding/epoch/generation, selected-network changes and shared-budget result normalization. Final pre.6: 30/30 instrumentation tests passed, including existing MDM/configuration/relay/budget regressions. Full Note9 continuity acceptance passed again on the exact pre.6 APK.
- Note9 actual TUN/REALITY/Wi-Fi test: two healthy nodes and one refused port; local coordinator results, isolated MDM start/duplicate/cancel adapter, wire metadata stripping and epoch-resume filtering. Two independent paths share one working session. Stopping one carrier retained one destination TCP accept, 360 exchanges, max RTT 18 ms; subsequent UDP passed. The isolated MDM test does not enroll the real phone or enable its paused MDM.
- WEB: 32-row browser fixture, local dates, filters, neutral canceled rows, desktop and narrow viewport. Authentication/CSRF and page rendering have server tests.

## Found and corrected during verification

Numeric endpoints must bypass Android DNS lookup. A temporary fixture initially lacked the compatibility response required by its newly supplied control URL; its HTTPS echo and mTLS gateway were healthy. The fixture now supplies that response. These initial failures were not accepted as passing evidence.

Independent review led to stored MDM ownership checks, cancellation on ownership/network changes, post-exchange LTE-budget classification, TLS authentication classification and neutral unfinished rows. Epoch activation also marks a previous remote run interrupted on the server so stale UI state cannot block subsequent checks.

## Limits

Actual acceptance used Wi-Fi. Physical LTE exhaustion and a carrier-blocked mobile network were not exercised here; budget/network policies have deterministic tests. Public Note12 MDM round-trip and its third-party nodes require the user's upgrade. Probe success proves the measured HTTPS exchange and, when configured, a separate relay data-plane exchange; it does not promise throughput, UDP support, or all sites. The working VPN is not reconfigured by probing.

## Engineering decisions

The echo route follows the existing public router in `cmd/server/public.go`. Total duration is reported without inventing a VLESS handshake timestamp. A bounded `probeSources` catalogue carries source fingerprints unavailable in the redacted configuration. Local probes may use the configured gateway HTTPS origin and verified CA; remote probes use the MDM origin. No TLS verification bypass is used.
