# Android underlying-network recovery implementation plan

Goal: prevent legacy applications from retaining offline state after Wi-Fi loss.
Architecture: a per-VPN-run publisher observes physical/default network changes independently of transport workers. Use Android default tracking (null) when the selected path is the system default; retain explicit live paths for other selections. Filter lost networks before publication and guard late callbacks after shutdown. Preserve explicit empty lists for paused exits.
Stack: Kotlin, Android ConnectivityManager/VpnService, JUnit, existing Gradle build.
Spec: docs/testing/tcp-stall-diagnosis-2026-11-16.md, live reproduction and VPN-only recovery.

User authorized the fix and autonomous implementation. Reuse the existing worktree.

- [x] Add regression tests for default tracking, lost selected path, delayed transport updates, multi-path ordering, no default, empty/paused selection.
- [x] Observe the old explicit-list behavior fail those tests.
- [x] Implement a pure publication policy and per-run Android observer; route single, multiple and bond/demux callers through it; close observers on shutdown.
- [x] Run unit tests, build and lint; review lifecycle, callback failure, metered/explicit selection and VPN capture boundaries.
- [x] Install debug update on Note12 preserving data, restore its VPN, and repeat Wi-Fi off/on while observing Telegram and tunnel state. Report device validation separately from unit validation.

Decisions: no server or Go transport change. No automatic VPN/TUN restart on normal migration. Lost requested paths temporarily use Android default metadata until a live transport choice exists. This changes reported availability only; socket binding, routing and LTE budget remain owned by existing code.

Execution record (2026-11-16): old explicit-list behavior failed 6/9 regression cases. The first stateful implementation passed 9/10 but failed the default-switch-before-worker case. Added a default-following latch for an unchanged single live path; a new selection, explicit pause, or restored multipath list can leave default-following mode.

Ruling: aggregate bond/demux path lists per exit in MultipleVpnController, instead of allowing a session to overwrite the whole VPN list — the new shared publisher must preserve paths owned by other active exits. All callbacks are generation-guarded and dispatched to the main thread; the per-run publisher ignores updates after close.

Final review: identified active bond/demux total path loss being confused with an explicit pause, including multi-exit resume. Regression activeBondWithoutPathsFollowsRecoveryBeforeWorkerCatchesUp failed (13 tests, 1 failure) before correction. Publication now distinguishes null (active, awaiting paths) from [] (all exits intentionally paused); resuming an exit publishes immediately.

Final validation: full host JUnit 15/15, including 13 policy cases; assembleDebug
and lintDebug succeeded (0 lint errors). Fresh review P1 fixed with RED -> GREEN.
Installed version 80 / 0.9.5-pre.16-dev on Note12 with data retained. Two ordinary
round trips plus an 8-second total network gap recovered without Telegram/VPN
restart; same VPN NetID and server QUIC session throughout, with persistent
Telegram TCP tuple/inode evidence. Restored Wi-Fi, per-SIM data and screen timeout.
Details and validation limits are in the diagnosis report. No server deployment
or transport changes. Multipath device validation remains outside this ordinary
QUIC field test; aggregation is reviewed and underlying selection/pause policy
is unit tested.
