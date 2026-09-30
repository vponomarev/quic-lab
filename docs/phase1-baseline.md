# Phase one baseline

Existing development snapshot; baseline host tests and Android builds verified on 2026-09-30. Device execution remains pending.

Baseline commit: 4ed5b1f707baac101663607d6a176c859697f961

Source files copied without modification: 43

## Snapshot SHA-256

```json
{
  "android/app/build.gradle.kts": "d43b767468781ba7cb7b9c07a9e1f78686995302b5d8eb5aef2bc9827b4d04fd",
  "android/app/src/androidTest/java/ru/vpnc/quiclab/BondDeviceTest.kt": "d90cf474780cc831d25602403800aa75845085491f6f302d096eeee4fa6a06b9",
  "android/app/src/androidTest/java/ru/vpnc/quiclab/VpnReserveTest.kt": "fa5d0bae8b4d8f2f780f96bb50dfbb35ed362115bdf9ae7aedf432e9d8d1596d",
  "android/app/src/androidTest/java/ru/vpnc/quiclab/VpnRttTest.kt": "2ca8dfda2963bbcb80e09dfaaf07515204a8f31ffab3bf296225fa1595fe5f4f",
  "android/app/src/main/java/ru/vpnc/quiclab/Diagnostics.kt": "9f224d219e2c43c72d2c7584d24f3b08cdd4cf87b84f57bbaeda85f0dd7099e0",
  "android/app/src/main/java/ru/vpnc/quiclab/LabVpnService.kt": "1c205098c209503ef5c164452b741070039e7a37b2338f84bf078e125b398fc8",
  "android/app/src/main/java/ru/vpnc/quiclab/LatencyChart.kt": "85fd7c53be3478a339e93827fcf87919384e9ad84fca39e5bb27b5b10fc5a4fc",
  "android/app/src/main/java/ru/vpnc/quiclab/MainActivity.kt": "ef13f43044fc9678cf748384def9c45a26e4d2559748a1cb2811bf9177111efb",
  "android/app/src/main/java/ru/vpnc/quiclab/MultipleVpnController.kt": "725668e63a0f17fac7a7f52f61b219eb2ad22ed1a05ccfb0a722f32d9dab08ea",
  "android/app/src/main/java/ru/vpnc/quiclab/QuicSession.kt": "1d4133d3cdae4aadfada9aac193bf8d5aadbea590adfd00c539308692850e97c",
  "android/app/src/main/java/ru/vpnc/quiclab/VpnActivity.kt": "efab6a0b294c16f69a2ff4fc3cfc7dc79d599a9934ecf5cae789044d868f481a",
  "android/app/src/main/java/ru/vpnc/quiclab/VpnReserveSettings.kt": "2b2dcce3b674d77629475b8e4aeb37a26766bda7479a6eff69e505562453a8af",
  "android/app/src/main/java/ru/vpnc/quiclab/VpnRttSettings.kt": "731b92e22db85aec3d327b1f19e4cb92d880a4a70ef3d87867906cffa0e7c1a5",
  "android/app/src/main/java/ru/vpnc/quiclab/VpnSession.kt": "ea257b8771e9e33833bc394b97d4fc4ed0ae4ab670b62cce722445b0d76ebd5f",
  "android/probe/src/main/java/ru/vpnc/quicprobe/ProbeActivity.java": "0997d89d9f4e47beecd338766d284f3bcea135a26f244975148cc3246d2a5967",
  "cmd/demux/main.go": "6c6846c2c49e4516be0bd5da42fc8bb842f05d36165306d6f33de1a401a97ee2",
  "cmd/server/config.go": "b44e1e5f1a8e8044f08b21b2fa3ce05f9e7137f389f2e6e263f382877f7fa547",
  "cmd/server/main.go": "86ce1b7ffbeb797a11a137c330a99ae7a50f65972553c75080f4a152580f5db8",
  "cmd/server/public_tls.go": "422b3a3e0a24b97165e11852dd9ca4f01f5b18bc42a1d8a4ff09c98626a4afef",
  "cmd/server/public_tls_test.go": "1ebf4cf839828754ff57cbd2e19b5c551fbc39bd580c1dfc76d0dca39c191b49",
  "docs/bond-layers.md": "29e27f957823bdd889461458671dcec04e33d4d8a2e188c19a2b015e75339a4b",
  "docs/demux.md": "f0941904f5af6f8b7f99e17a7babd80bbee10a919ae37da95dc13a75fad8730b",
  "docs/max-availability.md": "a8abac534d3a7cadb04190ae7225060c11183a41eaa0ac258a6858bc03af9948",
  "docs/public-tls-compatibility.md": "7e9c35cca2ae9a3c2a38bec8092bc2ddb0237a958e52445027b8b4828f034988",
  "docs/vpn-reserve.md": "afc4721f1988edfc632d2f1f758a366309fa9ac1a959afbb4994de7bf97bf78e",
  "docs/vpn-rtt.md": "068a2e3fbaf02905d97541f6cd81aa115e3a21589cd4ff1c4fb2a5ca5f5ac406",
  "internal/bond/mux.go": "f4dc972d6e011b624ce4051fd9520c9b1d8ceee305068c4198fc77775753054b",
  "internal/bond/session.go": "ba6f01ec11c0d22368e4823a11fc30951c8079cf2890c283dd39c7e54316c3e4",
  "internal/bond/session_test.go": "5acb849494167e1ec543e60eb1f194616a84d520af6e0434b2a5f975f9b11edd",
  "internal/bondquic/path.go": "e22619211acc390c7f5501a351d385f54a22a1a211eb8f4e3aa3fc4753cbf1f5",
  "internal/gateway/bond.go": "5da8093b949d18699841d667e3f2425d836ed56e1693a276096cb42df32c1d85",
  "internal/gateway/datagram.go": "eb4a0fe0268e4511db266194537c382abf195870b6d26bc6b68eda59f4459cbc",
  "internal/gateway/server.go": "38bd8cbd6f74ae89c9a6b3341ed78f968376a4a6d7960230c462ea1e573e80ce",
  "mobile/client.go": "e53d39f57392ba2cf6e83cb2197adb81d974dd7f2b2c25892fad958b8cdad4ed",
  "mobile/gateway.go": "f244b38cf237b2edc1959cd87334ae72c82eaf37d9c99dbfc099d8f4b6b95122",
  "mobile/gateway_backend.go": "7194722c967302cb4642b0cf4b1f25ff11f1839bbaa6daa5415e1e9e98b805b9",
  "mobile/gateway_bond.go": "9ad96f004725a3230450daabdd24c6964525757cb8a4db53f6a1fb38bb74787e",
  "mobile/gateway_bond_test.go": "c7a9c08e71be539a653291d59082ce00c75ca1ceb1359d889f15585d9fc5c95d",
  "mobile/gateway_datagram_test.go": "7d97057dba349f87e4517fcc593c66746fa4c7d80efbc0373b6b075072bb019e",
  "mobile/gateway_recovery_test.go": "44a406f8129159a9ff15e934e966a27e4af58949985171c8490d0afa86677717",
  "mobile/gateway_rtt_test.go": "03eefa861841fac6d0d60ced0e90e5e791e63c2c2916916916f45aefb11e6983",
  "mobile/probe_policy.go": "19d7fd48ede17091d5d188d19ee4e2b03413c6712779bed3d7162d8bc0a82fe6",
  "mobile/probe_policy_test.go": "5dfca562e5f5feb08b1ea79a53f0525106acaab7eb31e3809c254a8c65c63768"
}
```

## Verified checks

- Linux, isolated source snapshot: Go 1.26.8, gcc present.
- `go test ./internal/bond ./internal/bondquic ./internal/gateway ./internal/admin ./mobile ./cmd/server ./cmd/demux -count=1 -timeout=120s`: exit 0. bondquic and cmd/demux have no test files; no coverage claim for them.
- `go vet ./...`: exit 0.
- `go test -race ./internal/bond ./internal/gateway ./internal/admin ./mobile -count=1 -timeout=180s`: exit 0, all four packages passed.
- Windows build tools: `scripts/build-android.ps1 -BuildApk`: AAR/APK and lintDebug successful.
- Windows build tools: `android/gradlew.bat -p android assembleDebugAndroidTest --console=plain`: successful. This builds tests but does not execute them on a device.
- Android instrumentation not run: phone is disconnected by default, per owner instruction. Request connection when needed; do not mark device-dependent tasks complete from compilation alone.
- No running server service, network routes or deployment changed. Linux test processes used an isolated source directory; Windows did not run Go test binaries.

## Execution handoff

Owner authorized development. Choice between task subagents and inline execution was requested and is still pending; baseline preparation is complete independently of that choice. First implementation task is A1. No new product behavior has been implemented in this baseline step.