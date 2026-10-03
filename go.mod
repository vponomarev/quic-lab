module quiclab

replace github.com/quic-go/quic-go => ./third_party/quic-go

go 1.26.0

toolchain go1.26.8

require (
	github.com/amnezia-vpn/amneziawg-go v0.2.20-0.20260724121833-457d920a1a7d
	github.com/coder/websocket v1.8.15
	github.com/quic-go/quic-go v0.63.0
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	github.com/xjasonlyu/tun2socks/v2 v2.6.0
	github.com/xtaci/smux v1.5.57
	github.com/xtls/xray-core v1.260327.0
	golang.org/x/mobile v0.0.0-20260908204917-8b95e45f8d3e
	golang.org/x/net v0.59.0
	golang.org/x/sys v0.48.0
	google.golang.org/protobuf v1.36.11
	gvisor.dev/gvisor v0.0.0-20260122175437-89a5d21be8f0
)

require (
	github.com/andybalholm/brotli v1.0.6 // indirect
	github.com/apernet/quic-go v0.59.1-0.20260217092621-db4786c77a22 // indirect
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/juju/ratelimit v1.0.2 // indirect
	github.com/klauspost/compress v1.17.4 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/miekg/dns v1.1.72 // indirect
	github.com/pires/go-proxyproto v0.11.0 // indirect
	github.com/refraction-networking/utls v1.8.3-0.20260301010127-aa6edf4b11af // indirect
	github.com/sagernet/sing v0.5.1 // indirect
	github.com/xtls/reality v0.0.0-20260322125925-9234c772ba8f // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/exp v0.0.0-20240506185415-9bf2ced13842 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/time v0.12.0 // indirect
	golang.org/x/tools v0.50.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
	google.golang.org/grpc v1.79.3 // indirect
	lukechampine.com/blake3 v1.4.1 // indirect
)

replace gvisor.dev/gvisor => gvisor.dev/gvisor v0.0.0-20250523182742-eede7a881b20
