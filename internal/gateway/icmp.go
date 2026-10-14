package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"net"
	"net/netip"
	"quiclab/internal/icmprelay"
	"time"
)

// One authenticated, bounded stream per echo. Unknown/old servers reject the
// extension; the client never falls back to a local fabricated echo or direct IP.
func (s *Server) serveICMP(ctx context.Context, st Stream, req Request, id string) {
	ip, e := netip.ParseAddr(req.Address)
	if e != nil || !ip.Is4() || !s.Permitted(net.JoinHostPort(ip.String(), "1")) {
		WriteJSON(st, Reply{Error: "destination denied"})
		return
	}
	select {
	case s.icmpSlots <- struct{}{}:
		defer func() { <-s.icmpSlots }()
	default:
		WriteJSON(st, Reply{Error: "ICMP busy"})
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	st.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { st.Close() })
	defer stop()
	if WriteJSON(st, Reply{Session: id}) != nil {
		return
	}
	q, e := ReadPacket(st, icmprelay.MaxMessage)
	if e != nil || !icmprelay.ValidMessage(q, 8) {
		return
	}
	dial := s.DialContext
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	out, e := dial(ctx, "ip4:icmp", ip.String())
	if e != nil {
		return
	}
	defer out.Close()
	stopOut := context.AfterFunc(ctx, func() { out.Close() })
	defer stopOut()
	out.SetDeadline(deadline)
	wire := append([]byte(nil), q...)
	var ident [2]byte
	if _, e = rand.Read(ident[:]); e != nil {
		return
	}
	copy(wire[4:6], ident[:])
	wire[2] = 0
	wire[3] = 0
	binary.BigEndian.PutUint16(wire[2:4], icmprelay.Checksum(wire))
	if _, e = out.Write(wire); e != nil {
		return
	}
	buf := make([]byte, 65535)
	for {
		n, e := out.Read(buf)
		if e != nil {
			return
		}
		p := buf[:n]
		// IPConn.Read includes the IPv4 header; custom packet connections may
		// return only ICMP. Validate the source/header before removing it.
		if len(p) >= 20 && p[0]>>4 == 4 {
			h := int(p[0]&15) * 4
			if h < 20 || h > len(p) || int(binary.BigEndian.Uint16(p[2:4])) != len(p) || p[9] != 1 || icmprelay.Checksum(p[:h]) != 0 || netip.AddrFrom4([4]byte(p[12:16])) != ip {
				continue
			}
			p = p[h:]
		}
		if !icmprelay.ValidMessage(p, 0) || !bytes.Equal(p[4:], wire[4:]) {
			continue
		}
		copy(p[4:6], q[4:6])
		p[2] = 0
		p[3] = 0
		binary.BigEndian.PutUint16(p[2:4], icmprelay.Checksum(p))
		WritePacket(st, p)
		return
	}
}
