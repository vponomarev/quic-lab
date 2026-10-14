package netstack

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"gvisor.dev/gvisor/pkg/buffer"
	"net/netip"
	"os"
	"quiclab/internal/icmprelay"
)

type icmpPending struct {
	request, wire []byte
	reply         chan []byte
}

// ExchangeICMP sends an actual IPv4 packet through the encrypted device. Only
// the source and echo identifier are translated; TTL and router errors survive.
func (n *Net) ExchangeICMP(ctx context.Context, request []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case <-n.done:
		return nil, os.ErrClosed
	default:
	}
	_, _, ok := icmprelay.Parse(request)
	if !ok || request[8] == 0 || !n.localIPv4.Is4() {
		return nil, errors.New("invalid IPv4 echo")
	}
	wire := append([]byte(nil), request...)
	h := int(wire[0]&15) * 4
	local := n.localIPv4.As4()
	copy(wire[12:16], local[:])
	wire[10] = 0
	wire[11] = 0
	binary.BigEndian.PutUint16(wire[10:12], icmprelay.Checksum(wire[:h]))
	n.icmpMu.Lock()
	if len(n.icmpPending) >= 32 {
		n.icmpMu.Unlock()
		return nil, errors.New("ICMP busy")
	}
	if n.icmpPending == nil {
		n.icmpPending = make(map[uint16]*icmpPending)
	}
	var id uint16
	for {
		var b [2]byte
		if _, e := rand.Read(b[:]); e != nil {
			n.icmpMu.Unlock()
			return nil, e
		}
		id = binary.BigEndian.Uint16(b[:])
		if n.icmpPending[id] == nil {
			break
		}
	}
	binary.BigEndian.PutUint16(wire[h+4:h+6], id)
	wire[h+2] = 0
	wire[h+3] = 0
	binary.BigEndian.PutUint16(wire[h+2:h+4], icmprelay.Checksum(wire[h:]))
	p := &icmpPending{append([]byte(nil), request...), wire, make(chan []byte, 1)}
	n.icmpPending[id] = p
	n.icmpMu.Unlock()
	defer func() { n.icmpMu.Lock(); delete(n.icmpPending, id); n.icmpMu.Unlock() }()
	view := buffer.NewViewWithData(wire)
	select {
	case n.incomingPacket <- view:
	case <-ctx.Done():
		view.Release()
		return nil, ctx.Err()
	case <-n.done:
		view.Release()
		return nil, os.ErrClosed
	}
	select {
	case r := <-p.reply:
		return r, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-n.done:
		return nil, os.ErrClosed
	}
}
func (n *netTun) receiveICMP(packet []byte) bool {
	if len(packet) < 28 || packet[0]>>4 != 4 || packet[9] != 1 {
		return false
	}
	h := int(packet[0]&15) * 4
	if h < 20 || h+8 > len(packet) || int(binary.BigEndian.Uint16(packet[2:4])) != len(packet) || binary.BigEndian.Uint16(packet[6:8])&0x3fff != 0 || icmprelay.Checksum(packet[:h]) != 0 || netip.AddrFrom4([4]byte(packet[16:20])) != n.localIPv4 {
		return false
	}
	n.icmpMu.Lock()
	defer n.icmpMu.Unlock()
	for _, p := range n.icmpPending {
		wh := int(p.wire[0]&15) * 4
		r := icmprelay.RestoreResponse(p.request, p.wire[wh:], netip.AddrFrom4([4]byte(packet[12:16])), packet[8], packet[h:])
		if r != nil {
			select {
			case p.reply <- r:
			default:
			}
			return true
		}
	}
	return false
}
