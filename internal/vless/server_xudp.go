package vless

import (
	"encoding/binary"
	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/mux"
	xnet "github.com/xtls/xray-core/common/net"
	"io"
)

// serverXUDPReader keeps UDP associations inside their authenticated outer
// connection. Upstream's global-ID cache is process-wide, not keyed by user.
// Removing the optional resume ID preserves datagrams but deliberately disables
// cross-connection UDP association reuse. This also makes revoke deterministic.
// Metadata is bounded by upstream's 512-byte parser; payload is streamed in
// buf.Size chunks, including datagrams larger than one buffer.
type serverXUDPReader struct {
	source    *buf.BufferedReader
	policy    *serverContext
	remaining int32
}

func newServerXUDPReader(r buf.Reader, p *serverContext) *serverXUDPReader {
	return &serverXUDPReader{source: &buf.BufferedReader{Reader: r}, policy: p}
}
func (r *serverXUDPReader) Interrupt()   { common.Interrupt(r.source.Reader) }
func (r *serverXUDPReader) Close() error { return r.source.Close() }
func (r *serverXUDPReader) ReadMultiBuffer() (buf.MultiBuffer, error) {
	b := buf.New()
	fail := func(e error) (buf.MultiBuffer, error) { b.Release(); return nil, e }
	if r.remaining > 0 {
		n := min(r.remaining, int32(buf.Size))
		if _, e := b.ReadFullFrom(r.source, n); e != nil {
			return fail(e)
		}
		r.remaining -= n
		return buf.MultiBuffer{b}, nil
	}
	var m mux.FrameMetadata
	if e := m.Unmarshal(r.source, false); e != nil {
		return fail(e)
	}
	switch m.SessionStatus {
	case mux.SessionStatusNew, mux.SessionStatusKeep:
		if m.SessionStatus == mux.SessionStatusNew || m.Target.IsValid() {
			if m.Target.Network != xnet.Network_UDP || !r.policy.permits(m.Target) {
				return fail(ErrDeviceRevoked)
			}
			b.UDP = &m.Target
		}
	case mux.SessionStatusEnd, mux.SessionStatusKeepAlive:
	default:
		return fail(ErrDeviceRevoked)
	}
	m.GlobalID = [8]byte{}
	if e := m.WriteTo(b); e != nil {
		return fail(e)
	}
	b.UDP = nil // The outer link is a byte stream, not a packet writer.
	if m.Option.Has(mux.OptionData) {
		var length [2]byte
		if _, e := io.ReadFull(r.source, length[:]); e != nil {
			return fail(e)
		}
		b.Write(length[:])
		r.remaining = int32(binary.BigEndian.Uint16(length[:]))
	}
	return buf.MultiBuffer{b}, nil
}
