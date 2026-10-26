# MDM profile and transport selection

User approved: keep MDM and radio collection active while choosing an allowed profile and QUIC/HTTPS/AWG transport. Confirm pause with explicit warning that collection stops and pending radio records are deleted.

Approach: extend the existing delegated profile selection policy, with a separate transport action on connection cards. General configuration editing remains locked. Only provisioned transports with endpoints and credentials are offered. VPN must be stopped, as for existing profile selection; MDM remains active. Persist choice in encrypted managed runtime. Preserve on ordinary sync only while policy revision and server transport settings/identity are unchanged. Explicit server changes and revocation win. External layer detach restores personal configuration.

Alternative of unlocking the whole editor rejected: grants unrelated configuration changes. Separate transport profiles alone do not satisfy selecting transport within one provisioned server profile.
