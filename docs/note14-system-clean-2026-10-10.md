# Note14Pro: остановка VPN/MDM 2026-10-10

Устройство Xiaomi 24116RACCG, Android 16, QUIC Lab 0.9.1-pre.10 (50). Read-only ADB investigation, 15:38–15:41 MSK. Settings and services were not changed.

- Server: last radio received 13:50:04 MSK, MDM report 13:50:13, AWG last contact 13:50:22.
- ApplicationExitInfo: process 25050 killed 13:50:29.411 MSK, reason 13 OTHER KILLS BY SYSTEM, description OneKeyClean, importance 125. Other historical OneKeyClean/GarbageClean terminations exist. No evidence identifying Family Link as initiator.
- Current process 5882 exists, cached-empty (state 19), no running MdmService/MdmTelemetryService. LabVpnService record has app=null: not evidence of a live VPN. Package stopped=false, suspended=false.
- MIUI appop 10008 (autostart, as previously investigated in project): ignore, with rejection about 13:50. RUN_ANY_IN_BACKGROUND allow; fine/background location granted. This is distinct from Family Link whitelist.
- mdm-radio-options: server_enabled=true, expected binding retained, last_upload 1791629403909. wifi_only absent (code default false). Server geo/telemetry consent active; budget limitBytes=0 (unlimited).
- At 15:33:02 server received 34 delayed diagnostic summaries from 13:51:01–15:32:36; all report LabVpnService.active=false. Gaps in summaries mean continuous process execution cannot be inferred. No new radio records or MDM report at 15:34.
- Code inspection: DiagnosticsDelivery can initialize Diagnostics without MdmRuntime.restore. MDM restore entry points are activities and boot/unlock/package-replaced receiver; diagnostics background delivery does not by itself restore MDM. This explains why successful log delivery is not proof that MDM recovered; exact failed service-restart path needs targeted reproduction.

Conclusion: primary observed outage starts with OS process cleanup, followed by absent VPN/MDM services. Autostart restriction is an independently observed recovery obstacle. Not an established LTE/DNS failure. Family Link causality unproven. No changes to supervision or OS restrictions made. Future work: user checks per-app autostart/background policy; reproduce supported process-recovery lifecycle and improve reporting of OS exit reason and inactive MDM service. Do not claim a code fix or complete recovery from these reads.

## Recovery implementation and live test, 2026-10-10

- 0.9.2-pre.1 (52): Application entry restores active MDM even when process is created by background diagnostics; VPN foreground entry retries MDM restoration. Restore failures emit exception class without secrets. Manifest regression failed on original pre.10 and passed on patched client. 12 lifecycle/runtime tests passed.
- Enabled per-app MIUI autostart with user authorization. It returned to ignore after APK installation/instrumentation twice; the individual trigger is not yet isolated. Re-enabled after final installation. Family Link unchanged.
- User cleared recents at 15:52:04.037 MSK: OneKeyClean killed PID22306; PID22964 restored MDM and radio services by approximately 15:52:06. Server confirmed fresh reports and radio at 15:54:08. VPN service remained app=null despite resume=true, same boot24 and saved meter.
- 0.9.2-pre.2 (53): added VpnProcessRecovery at process entry and once MDM becomes foreground. It requests only same-boot interrupted sessions with existing VPN permission. Service rechecks durable resume eligibility when executing queued restart so explicit Stop wins. Existing meter epoch and used bytes retained. No new VPN start after reboot.
- Build/lint PASS. 19 instrumentation tests PASS on Android16: ProcessRecoveryTest, VpnBudgetPersistenceTest, VpnBudgetRunTest, MdmLifecycleTest, MdmRuntimeTest. New recovery test covers active VPN, missing permission, changed boot and explicit Stop.
- Pre.2 installed; all three services running PID24952 at 15:56, telemetry upload acknowledged; budget epoch e8288cb4-9683-4759-b3b6-0c20ba172d13 and used5227500 retained.
- Awaiting second user recents-clear on pre.2. Do not claim full live recovery until that test passes. No release/public download publication yet. Source changes remain uncommitted pending acceptance.

## Final live acceptance — pre.2

User cleared recents again at 15:58:38.028 MSK. ApplicationExitInfo confirms OneKeyClean killed PID24952; a new PID25719 owns actual MdmService, MdmTelemetryService and LabVpnService instances (all app references non-null), without reopening the application. Autostart remains allow.

Production verified: fresh AmneziaWG contact 15:58:51, radio sample 15:58:49 received15:58:51, subsequent MDM report15:59:17 with version0.9.2-pre.2 and vpnState=running, radio again15:59:22. Saved budget period and used5227500 are unchanged. This verifies recovery on this Android16/Xiaomi device with autostart allowed; it does not promise uninterrupted flows across process destruction or bypass vendor restrictions. Public release/download remains0.9.1; pre.2 is installed only on the test Note14Pro.
