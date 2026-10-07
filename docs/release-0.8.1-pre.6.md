# 0.8.1-pre.6
Android versionCode: 39.

Fix application update checks accidentally selecting validated operator IMS networks without public internet/DNS. Require INTERNET, VALIDATED and NOT_RESTRICTED capabilities, exclude VPN, prefer Wi-Fi.

Reproduced on Xiaomi 22101316UG: pre.5 update check selected operator IMS and failed with DNS error; the same live request passed with the fix. Four Android instrumentation tests passed including the live production metadata check and IMS/VPN/restricted-network eligibility regression. Build and lint passed. No DNS server override or VPN profile change.
