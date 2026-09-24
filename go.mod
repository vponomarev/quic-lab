module quiclab

replace github.com/quic-go/quic-go => ./third_party/quic-go

go 1.26.0

toolchain go1.26.8

require (
	github.com/quic-go/quic-go v0.63.0
	golang.org/x/mobile v0.0.0-20260908204917-8b95e45f8d3e
)

require (
	github.com/coder/websocket v1.8.15 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/net v0.59.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/tools v0.50.0 // indirect
)
