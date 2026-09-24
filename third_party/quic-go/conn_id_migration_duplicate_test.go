package quic

import (
	"github.com/quic-go/quic-go/internal/protocol"
	"github.com/quic-go/quic-go/internal/wire"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProbingCIDRetransmissionDoesNotRequeueOrRetire(t *testing.T) {
	var retired []uint64
	m := newConnIDManager(protocol.ParseConnectionID([]byte{0}),
		func(protocol.StatelessResetToken) {}, func(protocol.StatelessResetToken) {},
		func(f wire.Frame) {
			if r, ok := f.(*wire.RetireConnectionIDFrame); ok {
				retired = append(retired, r.SequenceNumber)
			}
		})
	f := &wire.NewConnectionIDFrame{SequenceNumber: 1, ConnectionID: protocol.ParseConnectionID([]byte{1})}
	require.NoError(t, m.Add(f))
	_, ok := m.GetConnIDForPath(1)
	require.True(t, ok)
	require.NoError(t, m.Add(f)) // retransmission after a lost ACK
	require.Empty(t, m.queue, "reserved CID must not become available for a second path")
	require.NoError(t, m.Add(&wire.NewConnectionIDFrame{SequenceNumber: 2, ConnectionID: protocol.ParseConnectionID([]byte{2})}))
	_, ok = m.GetConnIDForPath(2)
	require.True(t, ok)
	require.NoError(t, m.Add(f)) // now older than highestProbingID, but still in use
	require.Empty(t, retired, "a live probing CID must not be retired on retransmission")
}
