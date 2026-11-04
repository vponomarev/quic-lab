package approuting

// Statistics contains aggregate local counters only, never executable identities.
type Statistics struct {
	Enabled       bool   `json:"enabled"`
	DirectTxBytes uint64 `json:"direct_tx_bytes"`
	DirectRxBytes uint64 `json:"direct_rx_bytes"`
	Resolved      uint64 `json:"resolved"`
	Unknown       uint64 `json:"unknown"`
	Shared        uint64 `json:"shared"`
	Blocked       uint64 `json:"blocked"`
	Overloaded    uint64 `json:"overloaded"`
	TimedOut      uint64 `json:"timed_out"`
	DNS           uint64 `json:"dns"`
}
