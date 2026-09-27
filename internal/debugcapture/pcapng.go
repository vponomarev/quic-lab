// Package debugcapture implements bounded, opt-in teaching captures on isolated listeners.
package debugcapture

import (
	"encoding/binary"
	"errors"
	"io"
)

var le = binary.LittleEndian

func block(kind uint32, body []byte) []byte {
	n := (len(body) + 3) &^ 3
	b := make([]byte, n+12)
	le.PutUint32(b, kind)
	le.PutUint32(b[4:], uint32(len(b)))
	copy(b[8:], body)
	le.PutUint32(b[len(b)-4:], uint32(len(b)))
	return b
}
func section(link uint32) []byte {
	b := make([]byte, 16)
	le.PutUint32(b, 0x1a2b3c4d)
	le.PutUint16(b[4:], 1)
	le.PutUint64(b[8:], ^uint64(0))
	out := block(0x0a0d0d0a, b)
	b = make([]byte, 8)
	le.PutUint16(b, uint16(link))
	le.PutUint32(b[4:], 262144)
	return append(out, block(1, b)...)
}
func secrets(p []byte) []byte {
	b := make([]byte, 8+len(p))
	le.PutUint32(b, 0x544c534b)
	le.PutUint32(b[4:], uint32(len(p)))
	copy(b[8:], p)
	return block(10, b)
}

// readPCAP converts tcpdump's classic microsecond pcap stream into pcapng.
func readPCAP(r io.Reader, emit func([]byte) error, ready chan<- error) error {
	h := make([]byte, 24)
	fail := func(e error) error { ready <- e; return e }
	if _, e := io.ReadFull(r, h); e != nil {
		return fail(e)
	}
	var order binary.ByteOrder
	switch le.Uint32(h) {
	case 0xa1b2c3d4:
		order = le
	case 0xd4c3b2a1:
		order = binary.BigEndian
	default:
		return fail(errors.New("tcpdump must emit classic microsecond pcap"))
	}
	if order.Uint16(h[4:]) != 2 || order.Uint16(h[6:]) != 4 {
		return fail(errors.New("unsupported pcap version"))
	}
	link := order.Uint32(h[20:])
	if link > 65535 {
		return fail(errors.New("unsupported link type"))
	}
	if e := emit(section(link)); e != nil {
		return fail(e)
	}
	ready <- nil
	for {
		h = make([]byte, 16)
		if _, e := io.ReadFull(r, h); e != nil {
			return e
		}
		size, orig := order.Uint32(h[8:]), order.Uint32(h[12:])
		us := order.Uint32(h[4:])
		if size > 262144 || size > orig || us >= 1000000 {
			return errors.New("invalid capture record")
		}
		b := make([]byte, 20+int(size))
		ts := uint64(order.Uint32(h))*1000000 + uint64(us)
		le.PutUint32(b[4:], uint32(ts>>32))
		le.PutUint32(b[8:], uint32(ts))
		le.PutUint32(b[12:], size)
		le.PutUint32(b[16:], orig)
		if _, e := io.ReadFull(r, b[20:]); e != nil {
			return e
		}
		if e := emit(block(6, b)); e != nil {
			return e
		}
	}
}
