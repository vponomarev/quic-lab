package vless

import (
	"bytes"
	"encoding/binary"
	"github.com/xtls/xray-core/common/buf"
	"github.com/xtls/xray-core/common/mux"
	xnet "github.com/xtls/xray-core/common/net"
	"io"
	"testing"
)

func muxFrame(t *testing.T, target xnet.Destination, id [8]byte, status mux.SessionStatus, payload []byte) []byte {
	t.Helper()
	b := buf.New()
	defer b.Release()
	b.UDP = &target
	m := mux.FrameMetadata{SessionID: 3, SessionStatus: status, Option: mux.OptionData, Target: target, GlobalID: id}
	if e := m.WriteTo(b); e != nil {
		t.Fatal(e)
	}
	var length [2]byte
	binary.BigEndian.PutUint16(length[:], uint16(len(payload)))
	data := append([]byte{}, b.Bytes()...)
	data = append(data, length[:]...)
	return append(data, payload...)
}
func TestServerXUDPConnectionLocal(t *testing.T) {
	dest := xnet.UDPDestination(xnet.LocalHostIP, 1234)
	payload := bytes.Repeat([]byte{17}, 8192)
	raw := muxFrame(t, dest, [8]byte{1, 2, 3}, mux.SessionStatusNew, payload)
	raw = append(raw, muxFrame(t, dest, [8]byte{}, mux.SessionStatusKeep, []byte("next"))...)
	r := &buf.BufferedReader{Reader: newServerXUDPReader(buf.NewReader(bytes.NewReader(raw)), &serverContext{mode: "standalone"})}
	defer r.Close()
	for _, want := range [][]byte{payload, []byte("next")} {
		var m mux.FrameMetadata
		if e := m.Unmarshal(r, false); e != nil {
			t.Fatal(e)
		}
		if m.GlobalID != [8]byte{} {
			t.Fatal("global XUDP cache accessible")
		}
		if m.Target != dest {
			t.Fatal("destination changed")
		}
		var length [2]byte
		if _, e := io.ReadFull(r, length[:]); e != nil {
			t.Fatal(e)
		}
		p := make([]byte, binary.BigEndian.Uint16(length[:]))
		if _, e := io.ReadFull(r, p); e != nil || !bytes.Equal(p, want) {
			t.Fatalf("payload: %v", e)
		}
	}
}
func TestServerXUDPRejectsUnsupportedDestinations(t *testing.T) {
	for _, dest := range []xnet.Destination{xnet.TCPDestination(xnet.LocalHostIP, 80), xnet.UDPDestination(xnet.IPAddress([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}), 80)} {
		raw := muxFrame(t, dest, [8]byte{}, mux.SessionStatusNew, []byte("x"))
		r := newServerXUDPReader(buf.NewReader(bytes.NewReader(raw)), &serverContext{mode: "standalone"})
		if mb, e := r.ReadMultiBuffer(); e == nil {
			buf.ReleaseMulti(mb)
			t.Fatal("unsupported XUDP destination accepted")
		}
	}
}
