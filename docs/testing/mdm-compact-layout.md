# Compact MDM editor — 2026-10-13

Approved bounded redesign of the existing MDM and temporary editor. Three tinted configuration groups (connection/DNS, networks/LTE, diagnostics/research); Wi-Fi/LTE reserve matrix; two-column profile cards on desktop, responsive single column on phones; collapsible addresses, pools and credentials; sticky apply/preview panel. Device state remains visible above tabs. No change to server configuration schema or authorization.

Validation:
- node --check internal/admin/mdm_editor.js passed.
- Linux go test ./internal/admin -timeout 180s passed (10.955s); server build passed. Initial source transfer omitted local third_party/quic-go; included dependency and rebuilt successfully.
- Chromium synthetic-state acceptance: 1920x1080 three-column groups, two profiles; 1280 and 390px widths have no horizontal overflow. Changed LTE reserve checkbox and profile name, switched tabs, previewed and submitted to an intercepted endpoint. Draft retained, checkbox maps to cell_on, existing routes and app selection preserved; no JavaScript errors. Production phones received no test configurations.
- Desktop and mobile screenshots visually reviewed (ignored artifacts/research-live/mdm-layout-*.png). Test harness retained in ignored artifacts/research-live/mdm-layout-check.py.
- Deployed main server to quic-lab.vpnc.ru. Previous binary retained in /opt/quic-lab/rollback/mdm-layout-20261013. Public mdm-editor.js contains new layout; main, VLESS, AWG, transit services active. Binary SHA256 5fa9b392e6ab88a7f5918f1e137675aff046605d26073f4fcd9cd127152a00a2.

## Navigation follow-up

Status moved into Overview as horizontal current-state table; last five commands shown separately with timestamps. Telemetry tab requests authenticated no-store summary with one latest record, policy, consent and dropped count. Measured/received timestamps stay distinct. Wi-Fi/cell history links back to MDM device. Traffic tab links to account-level research (explicitly all VPN devices of owner) and independent Wireshark capture; users page has direct capture navigation.

Admin regression test first failed (summary returned HTML), then passed with implementation; full internal/admin suite passed (10.958s). Browser fixture verified hidden status outside Overview, command history, Wi-Fi/cells and two timestamps, account-filtered research and capture navigation, plus existing draft/submit/layout regression. Screenshots reviewed. Deployment rollback: /opt/quic-lab/rollback/mdm-nav-20261013; public script verified; all four services active. No production device configuration or capture was started by these checks.

## Overview refinement

Current state is now a vertical parameter/value table. VPN buttons moved beside current VPN status, preserving original button nodes and handlers across polling/rendering. Seven boolean MDM rights are always listed as disabled checkbox indicators, including denied rights; consent remains phone-owned. Browser fixture checked allowed/denied/disabled indicators and consecutive start/stop mocked commands after rendering; full admin suite passed (10.937s). Published with rollback at /opt/quic-lab/rollback/mdm-rights-20261013; public script verified.
