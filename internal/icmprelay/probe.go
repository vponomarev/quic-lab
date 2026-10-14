package icmprelay

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"golang.org/x/net/ipv4"
	"net"
	"net/netip"
	"time"
)

// Probe uses an unconnected source-bound socket: router ICMP errors can have a
// different source from the destination. Never use the host default exit when
// a configured transit source is unavailable.
func Probe(ctx context.Context, source string, request []byte) ([]byte, error) {
	target, q, ok := Parse(request)
	if !ok || request[8] == 0 {
		return nil, errors.New("invalid echo packet")
	}
	var local *net.IPAddr
	if source != "" {
		ip := net.ParseIP(source)
		if ip == nil || ip.To4() == nil {
			return nil, errors.New("invalid source")
		}
		local = &net.IPAddr{IP: ip}
	}
	conn, e := net.ListenIP("ip4:icmp", local)
	if e != nil {
		return nil, e
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	deadline := time.Now().Add(3 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	pc := ipv4.NewPacketConn(conn)
	if e = pc.SetTTL(int(request[8])); e != nil {
		return nil, e
	}
	if e = pc.SetControlMessage(ipv4.FlagTTL, true); e != nil {
		return nil, e
	}
	wire := append([]byte(nil), q...)
	if _, e = rand.Read(wire[4:6]); e != nil {
		return nil, e
	}
	wire[2] = 0
	wire[3] = 0
	binary.BigEndian.PutUint16(wire[2:4], Checksum(wire))
	if _, e = pc.WriteTo(wire, nil, &net.IPAddr{IP: net.IP(target.AsSlice())}); e != nil {
		return nil, e
	}
	buf := make([]byte, 65535)
	for {
		n, cm, peer, e := pc.ReadFrom(buf)
		if e != nil {
			return nil, e
		}
		from, ok := peer.(*net.IPAddr)
		if !ok || cm == nil || cm.TTL < 1 || cm.TTL > 255 {
			continue
		}
		addr, ok := netip.AddrFromSlice(from.IP)
		if !ok {
			continue
		}
		if reply := RestoreResponse(request, wire, addr.Unmap(), byte(cm.TTL), buf[:n]); reply != nil {
			return reply, nil
		}
	}
}
