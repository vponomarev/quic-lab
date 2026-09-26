package protocol

const ALPN = "quic-lab/1"

// Frame is one newline-delimited JSON message, not one QUIC packet.
type Frame struct {
	Transit          bool   `json:"transit,omitempty"`
	TransitSupported bool   `json:"transit_supported,omitempty"`
	TransitOK        bool   `json:"transit_ok,omitempty"`
	Seq              uint64 `json:"seq"`
	SentNS           int64  `json:"sent_ns"`
	ConnectionID     string `json:"connection_id,omitempty"`
	StreamID         int64  `json:"stream_id"`
	Peer             string `json:"peer,omitempty"`
}
