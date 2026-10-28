# Windows installer checks — 2026-10-28

Implemented in the existing Windows worktree; unrelated Android/server/admin and module edits excluded from the release commit.

- First-run manager tests initially failed with config_store_failed; missing storage now exposes a stopped unconfigured backend. Corrupt legacy/encrypted data still fails closed. First-save failure preserves the unconfigured state.
- Native storage tests initially rejected two absent stores; now both absent is first-run, either corrupt is an error.
- WPF acceptance initially failed because the dashboard offered Start for an empty installation. It now offers settings/import with full IPv4/automatic uplink defaults only for new profiles.
- Independent review caught empty arrays serialized as null; the actual read_settings JSON regression failed before cloneSettings preserved an iterable empty list.
- Independent review caught the original-user write into Inno's protected temp directory. Replaced with Windows WTS username/domain for the installer process's own session. Native Windows PowerShell 5.1 resolves the existing desktop owner; no elevated-identity or console-session fallback.
- Independent review caught service deletion before uninstall confirmation. Mutation moved to usUninstall; failure raises before file removal.
- Independent review caught permissive child ACL adoption. Recursive validation rejects foreign owners, reparse points, ordinary-user write access to payload and ordinary-user access to private state; controlled ACL tests cover accepted/rejected policies.
- Installer service command tests preserve a matching owner and reject foreign executable, data path, account and extra arguments. Payload tests reject tampering and path traversal.
- Go windowsclient/windowsengine tests and vet passed. WPF first-run, settings, imports and dashboard acceptance passed. Inno Setup 6.7.3 compiler is from the official release, SHA-256 9c73c3bae7ed48d44112a0f48e66742c00090bdb5bef71d9d3c056c66e97b732, with valid Pyrsys signature.

Limit: no elevated token was available to execute a real clean-install/upgrade/uninstall lifecycle. It is not claimed. The package remains a prerelease. Existing machine service, credentials, network routes and DNS were not altered by installer development.

Build with `scripts/build-windows-installer.ps1`, supplying pinned .NET SDK, Inno compiler, signed Wintun 0.14.1, its license and the complete dependency license bundle. Build from a clean exported source revision; output directory must not exist. The VERSION.txt records that revision. Runtime and Go dependencies remain fixed by project files.
