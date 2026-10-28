# Windows Installer Implementation Plan

> Execute inline with superpowers:executing-plans; use one independent final review.

**Goal:** Publish an installable Windows x64 beta without manual SID/profile provisioning.
**Architecture:** Inno Setup owns payload and uninstall registration; a bounded PowerShell helper owns service lifecycle. The service supports an explicit unconfigured state; the existing settings editor configures it.
**Tech Stack:** Go 1.26.8, WPF/.NET 10, Inno Setup 6, Windows PowerShell 5.1.
**Spec:** ../specs/2026-10-28-windows-installer-design.md

## Global constraints
- Existing names and DPAPI data remain compatible; no secrets shipped.
- VPN remains off after install/update; corrupt storage fails closed.
- Existing unrelated working changes are not included in the release commit.

## Review focus
- Missing versus corrupt saved settings; first save failure/restart.
- UAC with another administrator must preserve the original desktop owner.
- Foreign service paths, reparse directories and permissive state ACLs must be rejected.
- Update/remove must stop the exact service before replacing/removing its files.
- A new installation must offer imports and refuse VPN Start until configured.

## Tasks
- [x] Add failing manager/storage tests for no profile, first save/restart, bad legacy/new store; implement unconfigured state and protocol capability.
- [x] Add WPF first-run acceptance; show setup action and existing import editor, with full IPv4 and auto-uplink defaults only for new profiles.
- [ ] Add installer lifecycle helper, Inno project and reproducible packaging; exercise validation and isolated lifecycle, compile actual Setup.exe.
- [ ] Run affected Go tests/vet/build and WPF tests; review final diff, fix findings, commit only installer changes, publish new Windows prerelease and verify asset hashes.
