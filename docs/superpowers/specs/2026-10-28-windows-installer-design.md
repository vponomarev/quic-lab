# Windows installer

The owner wants to install the Windows client on another laptop by running an installer from GitHub Releases. The existing ZIP requires an administrator, a manually supplied SID and a valid engine.json before the GUI can work.

Build a Windows x64 Setup.exe using Inno Setup, bundling the Go service, signed Wintun and self-contained WPF UI. Setup installs to the existing Program Files location, registers the existing LocalSystem service, grants IPC to the original desktop user, creates Start menu/optional desktop shortcuts and registers an uninstaller. The installer must not start VPN. First launch exposes settings/import while no profile exists. A missing profile is distinct from an unreadable or corrupt profile; errors must never silently reset saved credentials.

Approaches considered: a self-extracting ZIP retains the manual setup problem; a custom WPF installer duplicates package/uninstall machinery; Inno Setup supplies the normal installation and removal UI. Inno Setup is the implementation choice. Initial install is the required scenario; existing installation updates preserve the service owner and private data, stop the service before replacing executable files, and leave VPN off. Removal stops/removes only the matching service and retains private settings for deliberate cleanup.

Keep unrelated Android/server work untouched. Preserve IPv4 beta limitations. Publish a new Windows beta with the setup executable, checksums and exact source revision; never put private profiles in the release. Verify first-run service/storage behavior, GUI onboarding, installer compilation and an isolated install/remove lifecycle where host permissions allow it. Report unverified hardware/OS scenarios honestly.
