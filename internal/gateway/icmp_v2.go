package gateway

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"quiclab/internal/icmprelay"
	"time"
)

func (s *Server) serveICMPv2(ctx context.Context, st Stream, req Request, id string) {
	ip, e := netip.ParseAddr(req.Address)
	if e != nil || !ip.Is4() || !s.Permitted(net.JoinHostPort(ip.String(), "1")) {
		WriteJSON(st, Reply{Error: "destination denied"})
		return
	}
	probe := s.ProbeICMP
	if probe == nil {
		if s.DialContext != nil {
			WriteJSON(st, Reply{Error: "ICMP TTL unavailable"})
			return
		}
		probe = func(ctx context.Context, p []byte) ([]byte, error) { return icmprelay.Probe(ctx, "", p) }
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
	p, e := ReadPacket(st, 1280)
	if e != nil {
		return
	}
	target, _, ok := icmprelay.Parse(p)
	if !ok || target != ip || p[8] == 0 {
		return
	}
	reply, e := probe(ctx, p)
	if e == nil && icmprelay.ValidateResponse(p, reply) {
		WritePacket(st, reply)
	}
}

// ExchangeICMP negotiates v2 per request. Explicit legacy rejection is the only
// fallback trigger; low TTL probes never use v1's non-TTL-aware forwarding.
func ExchangeICMP(ctx context.Context, open func(context.Context, string, string) (Stream, error), packet []byte) ([]byte, error) {
	target, q, ok := icmprelay.Parse(packet)
	if !ok || packet[8] == 0 {
		return nil, errors.New("invalid echo packet")
	}
	legacy := false
	st, e := open(ctx, "icmp-echo-v2", target.String())
	if e != nil && e.Error() == "unsupported network" && packet[8] >= 64 {
		st, e = open(ctx, "icmp-echo-v1", target.String())
		legacy = true
	}
	if e != nil {
		return nil, e
	}
	defer st.Close()
	stop := context.AfterFunc(ctx, func() { st.Close() })
	defer stop()
	if d, ok := ctx.Deadline(); ok {
		st.SetDeadline(d)
	}
	payload := packet
	if legacy {
		payload = q
	}
	if e = WritePacket(st, payload); e != nil {
		return nil, e
	}
	reply, e := ReadPacket(st, 1280)
	if e != nil {
		return nil, e
	}
	if legacy {
		reply = icmprelay.Reply(packet, reply)
	}
	if !icmprelay.ValidateResponse(packet, reply) {
		return nil, errors.New("invalid echo response")
	}
	return reply, nil
}
