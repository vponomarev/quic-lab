package mobile

import "quiclab/internal/protocol"

func emitTransit(emit func(string, map[string]any), f protocol.Frame, rtt float64) {
	if !f.TransitSupported {
		return
	}
	if f.TransitOK {
		emit("transit_echo", map[string]any{"rtt_ms": rtt})
	} else {
		emit("transit_probe_failed", map[string]any{})
	}
}
