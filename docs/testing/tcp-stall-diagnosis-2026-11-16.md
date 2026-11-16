# Android TCP stalls after network loss — initial diagnosis, 2026-11-16

Status: investigated, **production root cause not yet confirmed**. No runtime
code, live VPN/network settings, server services or installed applications
were changed. Source-only fault-injection tests ran on the authorized test host.

## Report and device identification

User reports Telegram staying in “connecting” after poor GSM connectivity;
occasionally YouTube too. Restarting the affected application helps. QUIC is
used in QUIC Lab; the separate AmneziaWG application does not show the problem.
The initially named Note15 was corrected by the user to the connected Note12 Pro
(Android model 22101316UG). A second affected phone is a 15T.

The Note12 Pro runs 0.9.5-pre.13/code78. Diagnostic SQLite was copied locally
without stopping the running VPN. Detailed flow logging is disabled. Raw phone
records remain under ignored `artifacts/tcp-stall-diagnosis-20261116/`; they are
not source or release assets. Its initial `note15/` artifact subdirectory name
predates the device-name correction and contains the Note12 Pro snapshot.

## Observations from the reported interval

User identified **14 November, 15:00–19:00 Moscow time** (12:00–16:00 UTC).
The retained Note12 Pro journal for this window contains:

| Event | Count |
| --- | ---: |
| Reconnect attempts | 166 |
| Connected / locally stopped old connection | 101 / 101 |
| Failed flow opens | 1,414 |
| Of these, `gateway disconnected` | 1,356 |
| Automatic migration/reconnect failures | 56 |
| Of these, capabilities HTTPS request timeout | 52 |
| Transit probe failures (no detailed cause) | 49 |

These are journal event counts, not unique application connections or a count
of Telegram failures. Entries do not identify the affected application/tuple.
`experiment stopped` is the local close reason during transport replacement;
it does not itself identify why the preceding transport became unhealthy.
Repeated loss and recovery of the phone-to-server path is directly observable.

For the 15T, available uploaded records start after the reported Saturday
window. In the last sampled hour (15 November 21:39–22:39 UTC), there are 24
path switches and 30 cellular-network available/lost event pairs. Recent
sequences show missing Wi-Fi replies for roughly 16 seconds, migration to LTE,
then return after Wi-Fi validation. This is distinct from the Note12's repeated
full-session replacement, and is not proof of the cause of Telegram's stall.

## Server checks

Correct SSH identity is the regular account with the existing test key; initial
root attempts were incorrect. SSH host checking remained enabled.
The gateway service has run continuously since 14 November 07:04 UTC, with
zero automatic restarts in the current systemd invocation. No matching kernel
messages were found in the reported four-hour window.

The configured global exit uses the legacy AWG transit: Linux TCP sockets bound
to the transit source address, followed by the AWG tunnel. Live `ss -tonp`
shows keepalive timers on these sockets. Thus the separate userspace-AWG-stack
keepalive observation below **does not explain this production setup**.

Gateway journal in the four-hour window contains 331 session opens/closes and
3,176 flow opens across users. Existing entries do not identify the user and
do not record TCP-close errors. Device-specific uploaded diagnostics surviving
retention start after the reported window, so these global counts cannot be
assigned uniquely to the Note12 or 15T.

## Controlled tests

Added `mobile/gateway_tcp_failure_test.go`,
`mobile/gateway_tcp_failure_linux_test.go` and
`internal/awg/netstack/tcp_liveness_test.go`.

1. Real upstream TCP RST reaches the QUIC client stream promptly, as EOF.
2. The same RST reaches an application-side TCP socket through the mobile TUN
   adapter, also as EOF. This verifies closure, not preservation of RST semantics.
3. Explicit QUIC reconnect also unblocks the existing application-side TCP
   read. The simple reconnect path does not leave that synthetic socket alive.
4. An in-memory packet filter drops only one established outbound TCP tuple.
   Its stream stalls, while the same QUIC session and a fresh connection to
   the same destination work. Uses the actual userspace AWG TCP stack, without
   AWG encryption, production NAT, host firewall changes or Telegram.
5. Outbound userspace-AWG TCP endpoint options show keepalive disabled; latent
   defaults are 2 hours / 75 seconds / 9 probes. This is a separate diagnostic
   observation, not the active legacy transit configuration.

All five tests passed three times with `-race`; test timeout 90 seconds:

```sh
go test -race ./mobile ./internal/awg/netstack \
  -run 'Test(QUICDestinationResetReachesStream|QUICHealthyWhileExistingEgressTCPBlackholed|TUNQUICDestinationResetUnblocksApplication|TUNQUICReconnectUnblocksApplication|OutboundTCPLivenessOptionsDiagnostic)$' \
  -count=3 -v -timeout=90s
```

Runtime implementation remains the release source; synthetic fixtures use
temporary credentials and isolated loopback/in-memory networking. The blackhole
test proves the architectural possibility, not that production NAT lost state.
Its observation window is short and does not establish the eventual TCP timeout.

## Fresh reproduced Wi-Fi → LTE → Wi-Fi incident

User reproduced the problem on 16 November: started VPN on Wi-Fi, left coverage,
YouTube recovered after 20–30 seconds, Telegram remained waiting, then recovered
without app/VPN restart when returning to the same Wi-Fi. The phone journal and
server session records give the following Moscow-time sequence (UTC+03:00):

| Time | Observed event |
| --- | --- |
| 22:29:27.841 | LTE network becomes available |
| 22:29:28–39 | Four QUIC migration attempts time out |
| 22:29:38.337 | Android reports the old Wi-Fi network lost |
| 22:29:40.264 | Old UDP socket reports network unreachable |
| 22:29:55.383 | Client starts a full reconnect through LTE |
| 22:29:55.651 | Server opens the new QUIC gateway session |
| 22:29:55.679–56.477 | Client selects LTE and reports connected |
| 22:30:35.544 | Android validates LTE |
| 22:30:55.973 | Server closes the old QUIC session and its remaining TCP flows |
| 22:47:32.812 | Android reports the returning Wi-Fi available |
| 22:47:34.263 | Android validates the returning Wi-Fi |
| 22:47:34.443 | First successful new TCP flow to Telegram address space |
| 22:47:35.284 | Client reports the standby Wi-Fi path ready |
| 22:47:41.508–41.708 | QUIC migrates from LTE to Wi-Fi |

The new session is correlated by the unique open at 19:29:55.651 UTC in both
the Note12-specific server diagnostic and the gateway journal. These two logs
use different session identifiers; they must not be joined by ID equality.
Scoped journal records are saved in ignored
`artifacts/tcp-stall-diagnosis-20261116/repro-evening/server-session-events.jsonl`.

Between the LTE reconnect and 22:47:34, that session has 64 successful TCP opens
but none to the Telegram ranges observed before/after this incident. The first
returning flows target 149.154.167.51 and 149.154.167.35. Their addresses fall in
the [official Telegram CIDR list](https://core.telegram.org/resources/cidr.txt).
Destination membership alone is not a per-UID attribution; the existing server
journal has no application identity. Moreover, `flow_open` is logged only after
an outbound dial and stream reply succeed, so absence of this entry does not
prove absence of connection attempts.

The phone has regular successful exit-IP checks during the LTE interval.
Summaries at 22:30:10 and 22:46:54 show RTT 40/113 ms, reply age 628/250 ms and
about 210 MB additional received traffic. This is a working tunnel with other
traffic, not a continuous 17-minute complete transport outage.

The first returning Telegram-address TCP opens precede the logged QUIC switch
by about seven seconds and even precede `standby_ready`. This strongly favors
investigating Android/application connectivity notification and local flow
recovery. It does not support assuming that merely restoring the old Wi-Fi
transport revived an old server-side TCP connection: the old server session
had already closed at 22:30:55, and the observed returning flows are new.
Actual application-level recovery was reported by the user; its exact instant
was not independently measured.

Read-only `dumpsys connectivity` confirms Telegram UID 10249 is in the VPN UID
set, with a default-network request attached to the VPN. The VPN has IPv4 default
routing, IPv6 unreachable, and bypass disabled. Current netpolicy rules for that
UID are NONE, and retained standby-rule changes during the incident are default,
not deny. These snapshots do not establish every historical OEM firewall rule.
The phone runs Telegram 12.10.6. No application/network settings were modified.

A relevant hypothesis comes from the
[upstream Telegram ApplicationLoader](https://github.com/DrKLO/Telegram/blob/master/TMessagesProj/src/main/java/org/telegram/messenger/ApplicationLoader.java):
its connectivity broadcast receiver invokes `checkConnection`, while its
network-capability callback resets the cached network type. This is a reason to
capture both event types and the application's online state, not proof about
the installed build or evidence that Telegram alone is at fault.

## Follow-up successful transitions

The user subsequently left Wi-Fi coverage three times; the first two did not
break Telegram, and Wi-Fi was explicitly disabled during the third attempt.
The user clarified that the original failed trip involved an elevator, where
both Wi-Fi and LTE may briefly have been unavailable. Simultaneous loss remains
a user hypothesis, not a radio-state measurement.

The fresh phone journal shows successful QUIC path switches to LTE at Moscow
22:59:46.890, 23:01:25.984 and 23:03:04.209, without full reconnects in that
interval. The switches completed roughly 0.37, 0.39 and 0.54 seconds after their
migration-start events. At 23:05, Android reports a validated CELLULAR|VPN network
with underlying LTE network 4822, and app-side TCP sockets to the observed
Telegram addresses are established with empty send/receive queues. Established
socket state alone is not proof of application-level health.

This supplies a useful comparison: successful migration preserves the QUIC
session; the failing incident required replacement after repeated migration
timeouts. Next controlled test requested: Wi-Fi off, mobile data off for 30-40
seconds, then mobile data on, without restarting Telegram or VPN. This models a
complete connectivity gap but is not identical to an RF outage in an elevator.
The snapshot is saved under ignored `repro-third/` beside the earlier artifacts.

## Live stall captured after explicit Wi-Fi off

The user then reproduced the stall simply by enabling and disabling Wi-Fi.
This overturns the idea that a full QUIC reconnect or a prolonged dual-network
outage is necessary: the session migrated successfully in this occurrence.

Phone and Android broadcast history, Moscow time:

| Time | Observation |
| --- | --- |
| 23:06:12.886 | Successful migration to Wi-Fi network 4823 |
| 23:06:27.947 | Wi-Fi network lost |
| 23:06:27.977 | Wi-Fi DISCONNECTED connectivity broadcast enqueued |
| 23:06:28.135 | Old UDP socket reports network unreachable |
| 23:06:28.384 | LTE CONNECTED connectivity broadcast enqueued |
| 23:06:28.391–28.453 | Two LTE broadcast dispatch/finish intervals in Android history |
| 23:06:28.413 | Client observes LTE network 4824 available |
| 23:06:28.777 | Client starts migration |
| 23:06:28.846 | Client emits active_network LTE |
| 23:06:28.908 | Client emits path_switched |
| 23:06:29.037 | Client observes LTE validated |

At 23:07 the VPN is validated on LTE network 4824; Telegram UID 10249 is in
its routing set and is foreground with netpolicy rules NONE. App-side TCP
sockets to the observed Telegram destinations are ESTABLISHED with empty
queues. Server-side candidate sockets have empty queues and keepalive ACKs;
one also has recent application data. Socket matching is by destinations/timing,
not a complete per-flow UID join. No wholesale server-side TCP blackhole is
visible in this snapshot.

A read-only UI hierarchy extraction independently confirmed the exact label
`Ожидание сети...` while the VPN remained functional. Only matching status labels
were emitted; the temporary hierarchy file on the phone was deleted. Artifacts
are in ignored `live-stall/`, including the network broadcast history, VPN and
UID state, app-side socket snapshot, SQLite journal and scoped server records.

### Leading mechanism: stale network availability after a notification race

Local code updates the VPN's explicit underlying network from the `selected`
callback only after migration/reconnect returns. Thus the LTE CONNECTED
broadcast above precedes even migration start, while the declared underlying
network can still be the lost Wi-Fi. The exact Binder update timestamp and
Telegram's API return values at broadcast delivery were not instrumented.

[AOSP ConnectivityService](https://android.googlesource.com/platform/packages/modules/Connectivity/+/refs/heads/main/service/src/com/android/server/ConnectivityService.java)
resolves legacy NetworkInfo through the first explicitly declared VPN underlying
network. If that Network no longer exists, it can return null; getNetworkInfo(type)
also returns early in that situation. Updating declared underlying networks
updates capabilities/statistics, without emitting a new legacy connectivity
broadcast in that handler.

[Telegram ApplicationLoader](https://github.com/DrKLO/Telegram/blob/master/TMessagesProj/src/main/java/org/telegram/messenger/ApplicationLoader.java)
checks availability from its connectivity broadcast receiver. Its default-network
capability callback only invalidates cached network type. The
[native ConnectionsManager](https://github.com/DrKLO/Telegram/blob/master/TMessagesProj/jni/tgnet/ConnectionsManager.cpp)
sets WaitingForNetwork when setNetworkAvailable receives false. Together these
sources explain how an early broadcast can leave Telegram waiting after the VPN
has already recovered, and why a later Wi-Fi broadcast can revive it before
QUIC itself switches paths. These upstream sources are not a decompilation of
the installed OEM framework/Telegram build; the mechanism is strongly supported,
with a controlled causal test still pending.

After preserving the live state, the user restarted only the VPN while
keeping Telegram and LTE unchanged, and confirmed that Telegram recovered.
The phone journal records VPN stop at Moscow 23:18:27.178 and connection through
LTE at 23:18:29.924. Android VPN network changed from 4596 to 4825, while the
underlying LTE network remained 4824. The recovery snapshot is in ignored
`vpn-restart-recovery/`. This confirms that restarting Telegram is unnecessary
for recovery. It strengthens the stale-availability hypothesis, but is not an
isolated proof: restarting VPN also replaces TUN/TCP and transport state.
During the initial investigation no runtime code was changed or deployed; the
only intervention in that phase was the VPN restart performed by the user. The
subsequent implementation and device validation are recorded below.

## Remaining validation before a runtime fix

The live stall and VPN-only recovery have now been captured. The leading cause
is the interaction between stale explicit VPN underlying-network metadata and
legacy connectivity notifications, supported by event order, the actual waiting
label, working LTE/VPN state and upstream Android/Telegram code. Direct recording
of the NetworkInfo values seen by Telegram at broadcast delivery is still absent.

A regression check must exercise loss of the currently declared physical network,
including a gap before a replacement is available, and an application that caches
availability from legacy broadcasts. Verify that no removed Network remains the
first declared underlying network while recovery is queued or blocked in a path
probe; also cover multiple exits sharing the Android VPN. Updating metadata only
after a successful migration is the current vulnerable ordering. Validate both
API notification behavior and end-to-end Telegram recovery before calling a fix
complete. Do not infer a server AWG/TCP fault solely from an app's waiting label.


## Implemented fix and Note12 validation, 2026-11-16

Installed `0.9.5-pre.16-dev` (versionCode 80) on Note12 via data-preserving APK
update. The per-run underlying-network publisher follows Android's default
network when a normal exit uses that default, without waiting for the transport
worker. It filters lost explicit paths and retains non-default/multiple live
paths. An unchanged old selection cannot re-pin the departed default network.
Explicitly paused exits use an empty list; active exits awaiting physical paths
use null/default-following. Per-exit multipath lists are aggregated, and late
callbacks are guarded by both exit generation and publisher lifecycle.

No Go, server, routing, TUN-recreation, or QUIC migration change was needed.
The Android metadata fix does not itself force TCP reconnects.

Verification:

- Original explicit-list behavior failed 6 of the first 9 regressions. The
  default-change-before-worker race failed the next regression before the latch
  was added. Review caught active bond path loss being mistaken for a pause;
  its regression also failed before correction.
- Final host JUnit: 15 tests passed, including 13 underlying-network cases.
  `assembleDebug` and `lintDebug` succeeded; lint reported 0 errors and 108
  warnings, none located in the touched Kotlin files.
- On Note12, Telegram PID 30456 remained unchanged throughout installation and
  the tests. After the one VPN restart required by APK replacement, VPN NetID
  4830 and server QUIC session `66353e65de360ac8e0857cc2ff444cc3` remained unchanged
  across all subsequent transitions. The server recorded no `gateway_closed`
  for this session through the final capture.
- Two normal Wi-Fi -> LTE -> Wi-Fi cycles completed. Recorded migration-start
  to path-switched intervals were 308, 235, 141 and 128 ms respectively. These
  measure the QUIC migration operation, not total Wi-Fi loss detection latency.
  Stable UI captures showed the normal Telegram heading instead of the waiting
  state; Telegram was not restarted and no messages were sent by the test.
- A deliberate roughly 8-second interval with Wi-Fi and mobile data both off
  was followed by LTE restoration. The same QUIC session migrated in 175 ms;
  Telegram showed its normal heading by the stable capture 11 seconds after
  mobile data was enabled. Wi-Fi return subsequently migrated in 192 ms.
- Telegram TCP socket `10.254.254.1:35998 -> 149.154.167.51:443`, inode
  `312308791`, was ESTABLISHED before the first LTE -> Wi-Fi return and persisted
  through the subsequent Wi-Fi/LTE cycle, complete network gap, and final Wi-Fi
  return. All four established Telegram sockets in the pre-gap snapshot retained
  the same tuples/inodes after LTE recovery. Other Telegram sockets were replaced
  by the application over time; the test does not claim every application will
  retain every socket indefinitely.
- Final state: Wi-Fi on as before testing, per-SIM mobile-data settings unchanged,
  screen timeout restored to 60000 ms, VPN running. App data/profiles retained.

Ignored artifacts are in `artifacts/tcp-stall-diagnosis-20261116/fix-validation/`.
One live SQLite copy collided with a journal write; it was a local snapshot error,
not evidence of corruption on the phone. Later snapshots and network/UI/server
observations completed. Transient UI dumps during switching occasionally failed;
only successful stable Telegram-package captures support UI conclusions.

Limits: the repeated user scenario and a short total gap were validated on this
Note12 in ordinary QUIC mode. Bond/demux and all-paused/resume semantics have code
review and policy regression coverage, but were not exercised end-to-end on a
configured multipath device in this iteration. A long outage that expires QUIC
still requires reconnecting its TCP streams; this fix does not add stream resume.
The installed OEM framework and Telegram build were not instrumented internally,
so the precise legacy-API value at broadcast delivery remains an inferred cause.
