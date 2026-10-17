# 0.9.5-pre.5 — VLESS shared exit (pre-release)

A VLESS profile/group can select an existing QUIC Lab profile as its shared HTTPS exit. Two independent VLESS paths join one authenticated bond session. The first activation reconnects VPN; subsequent carrier replacement preserves that session within its retention limits. Without a shared exit selection, standalone VLESS behavior is unchanged.

Android and the MDM editor expose “Напрямую” / “Через профиль QUIC Lab”. Only clients advertising `vless_relay_v1` receive this configuration. The gateway profile supplies its existing client identity; subscription credentials remain separate and encrypted on the device. Deleting/incompatibly changing a referenced profile is rejected rather than silently falling back directly.

The app and MDM show primary/reserve readiness. A lost logical session is distinguished from a surviving path replacement. Consent, app routing, physical network budget and reserve settings remain applicable.

Validation and limits: see [shared-exit verification](testing/vless-shared-exit.md). Linux reset/blackhole tests retain one destination TCP socket across 10 seconds of all-path outage. Note9 Wi-Fi/REALITY/TUN acceptance retains one TCP connection when a carrier is stopped. LTE handover in a blocked network and real third-party VLESS subscriptions with this shared exit require field acceptance; no universal no-interruption SLA is claimed. Note12 configuration is not automatically activated by publishing the APK.

Published to quic-lab.vpnc.ru on 2026-10-17. Public APK download SHA-256 verified against the build. Server service active after deployment. Rollback copy: `/opt/quic-lab/rollback/vless-shared-exit-pre5-20261017`. Note12 policy remains unchanged pending user upgrade and activation.

Artifacts (SHA-256):

- APK code 70: `b40f78972dea9767c4cf2819e0970dc23ddf192b70f228f11a1bc4af598d98b6`
- Linux server: `fdbd4050bea1ce7e879449d9ca30873728c5aeaa423f7941d49785a9365c3ba4`
