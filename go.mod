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
	golang.org/x/mobile v0.0.0-20260908204917-8b95e45f8d3e
	golang.org/x/net v0.59.0
	golang.org/x/sys v0.48.0
	gvisor.dev/gvisor v0.0.0-20250523182742-eede7a881b20
)

require (
	github.com/google/btree v1.1.3 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/time v0.11.0 // indirect
	golang.org/x/tools v0.50.0 // indirect
	golang.zx2c4.com/wintun v0.0.0-20230126152724-0fa3db229ce2 // indirect
)
