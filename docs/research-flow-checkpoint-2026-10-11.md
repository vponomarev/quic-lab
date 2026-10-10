# Research flow checkpoint — 2026-10-11

Scope: first Android → server table and JSONL checkpoint from the approved research-mode plan. Test client: 0.9.4-pre.2 (58), Redmi Note 9 Pro / Android 11. This is an isolated test build, not a production release.

## Implemented

- Separate voluntary research consent, off by default. For an active MDM binding its independent research right is authoritative; no second local grant is required. Enrollment/editing exposes the right; enrollment invitation defaults are unchanged.
- Own QUIC/HTTPS tunnels, including the shared gateway used by bond. Direct, AWG and VLESS forwarding do not install an observer.
- Asynchronous Android socket-owner lookup: UID, package names, labels; honest `unknown` or `shared_uid` when identity is unavailable/ambiguous. No PID-based guess.
- Random logical session and flow IDs, bounded owner/DNS/output queues, minute deltas and close records. Existing flows begin a new incomplete observation window after consent/policy enable; old bytes are not added retrospectively.
- Counters are **client logical application payload**, excluding tunnel framing, TCP/IP headers and retransmissions. TCP packet counters remain zero; UDP counters count datagrams. This is not server-side IP byte accounting.
- A versioned, bounded control stream inside the same authenticated tunnel. Device/user identities come from the client certificate. Consent generations cannot roll back within a logical session. Diagnostics have separate bounded admission from normal VPN flows.
- Default-off clients send no research requests. A pending revocation retries on the existing bounded control path; after acknowledgement it stays silent. Unsupported servers fail only the diagnostic stream.
- Ordinary DNS over UDP/53: questions, A/AAAA/CNAME answers, TTL, response code, transaction ID. Parsing is bounded; encrypted DNS and TCP DNS are not decoded. No inference that an IP address belongs uniquely to a DNS name.
- Authenticated `/lab/research`, filters and JSONL export. Default server policy is off; finite quota is mandatory; default retention is seven days. User policy remains subordinate to the global server switch and client consent.

## Operational limits of this checkpoint

Full capture, PCAPNG and Wireshark are deferred. The legacy administrator capture mechanism is separate; the new research control stream does not change its authorization model.

Store quota/retention changes after opening storage require a server restart; first configuration opens storage lazily. Live quota changes remain follow-up work. Research consent tombstones are bounded to 128 logical sessions per device and 4096 per server process; exhaustion disables only diagnostics until restart.

No durable client spool: failed delivery, revoke, process loss or VPN shutdown may discard pending metadata, including the last unflushed interval. A client gap is reported when collection resumes where possible; abrupt process death cannot guarantee a closing record. Previously persisted server data remains. Application names are client claims, not a trusted software inventory.

Exports contain untrusted device strings. LLM analysis is a manual export operation; nothing sends data to an LLM automatically. Traffic purpose cannot be reliably inferred from endpoint/DNS metadata alone.

## Acceptance evidence

Linux test host: 192.168.5.214, as required by the owner. Isolated fixture has independent certificates and data; production was not modified. Private profiles and raw test artifacts are Git-ignored under `artifacts/research-live/`.

- Complete Linux `go test ./... -count=1` passed. Race suites passed: gateway, research, admin, MDM, server command and mobile (including normal VPN regression tests).
- Android AAR/APK build and lint passed.
- Six on-device consent/owner tests passed.
- Physical QUIC run attributed `ru.vpnc.quicprobe` (UID 10238), with TCP payload counters matching the dedicated echo test. DNS question/response reached the server.
- Final-build QUIC and separate HTTPS acceptance passed. TCP echo totals exactly matched: QUIC 17,578 bytes each direction, HTTPS 19,824 bytes each direction. UDP echo matched 46/46 and 47/47 bytes; both DNS flows were 29/61 bytes with query/response records.
- Canonical JSONL validation passed: terminal `export_summary.complete=true`, known TCP app identity, no direct-app records, no off/revoked flows, all six consented flows closed. Desktop and 390px layout checked. Raw results/screenshots remain private under `artifacts/research-live/`.
- One final combined run passed consent/owner tests and QUIC, then hit a TCP dial timeout reaching the isolated HTTPS fixture. Wi-Fi was associated and the listener active afterward; root cause was not established. The separate HTTPS rerun passed without a product change. This is recorded as an unresolved intermittent test-environment/network observation, not claimed fixed.
- Original Note9 profile and MDM rights restored; working AmneziaWG/Wi-Fi connection verified with RTT 41.5 ms and exit IPv4. Temporary fixture credentials removed from the phone. Note12 and production unchanged.

Test harness lessons: reacquire versioned configuration preferences after consent changes; explicitly close VpnService because Android can keep it bound after stopService; remove previous probe-result files before reruns. Unique timestamped probe IDs now prevent stale result reuse. Initialize the application ConnectivityManager before UiAutomation to avoid its system-package context contaminating the Android 11 test process. These failures were in the test harness, not evidence of a VPN forwarding failure.

Review found and fixed a real consent-sampling race: an older blocked true read could overwrite a newer false notification. Ordered sample tickets reject stale results without holding the forwarding mutex across Android callbacks. The deterministic regression failed before the fix and the full mobile race suite passed after it.

The broad Go suite exposed an existing ordering assumption in TestSharedQUICBoundedDispatch. The test now waits for the server queue to fill before dialing the excess connection, rather than assuming client handshake completion orders server dispatch. Ten race-enabled repetitions and the complete suite passed.
