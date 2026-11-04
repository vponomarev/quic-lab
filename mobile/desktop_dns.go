//go:build (linux && !android) || windows

package mobile

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"golang.org/x/net/dns/dnsmessage"
	"io"
	"net"
	"net/netip"
	"time"
)

// ProbeDesktopDNS checks UDP DNS reachability through an AWG/VLESS generation.
// It bypasses application policy and never falls back to the host network.
func (g *Gateway) ProbeDesktopDNS(ctx context.Context, resolver string) error {
	a, err := netip.ParseAddr(resolver)
	if err != nil || !a.Is4() || !a.IsGlobalUnicast() {
		return errors.New("health DNS must be a unicast IPv4 address")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	g.mu.Lock()
	owner := g.ctx
	var d flowDialer
	if g.direct == nil {
		if g.cfg.Transport == "vless" && g.vless != nil {
			d = g.vless
		} else if g.cfg.Transport == "awg" && g.awg != nil {
			d = g.awg
		}
	}
	g.mu.Unlock()
	if owner == nil || owner.Err() != nil || d == nil {
		return errors.New("VPN DNS probe unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	stopOwner := context.AfterFunc(owner, cancel)
	defer stopOwner()
	c, err := d.DialContext(ctx, "udp4", net.JoinHostPort(a.String(), "53"))
	if err != nil {
		return err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := c.SetDeadline(deadline); err != nil {
		return err
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return err
	}
	name, _ := dnsmessage.NewName("health-" + hex.EncodeToString(nonce[:]) + ".invalid.")
	question := dnsmessage.Question{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}
	query := dnsmessage.Message{Header: dnsmessage.Header{ID: binary.BigEndian.Uint16(nonce[:2]), RecursionDesired: true}, Questions: []dnsmessage.Question{question}}
	raw, err := query.Pack()
	if err != nil {
		return err
	}
	if n, err := c.Write(raw); err != nil {
		return err
	} else if n != len(raw) {
		return io.ErrShortWrite
	}
	buf := make([]byte, 4096)
	n, err := c.Read(buf)
	if err != nil {
		return err
	}
	var reply dnsmessage.Message
	if reply.Unpack(buf[:n]) != nil || !reply.Response || reply.OpCode != 0 || reply.ID != query.ID || reply.Truncated || len(reply.Questions) != 1 || !desktopDNSQuestionEqual(reply.Questions[0], question) || (reply.RCode != dnsmessage.RCodeSuccess && reply.RCode != dnsmessage.RCodeNameError) {
		return errors.New("invalid VPN DNS response")
	}
	return ctx.Err()
}

func desktopDNSQuestionEqual(a, b dnsmessage.Question) bool {
	if a.Type != b.Type || a.Class != b.Class || a.Name.Length != b.Name.Length {
		return false
	}
	// DNS names compare without ASCII case; Unicode folding is not equivalent.
	for i := 0; i < int(a.Name.Length); i++ {
		x, y := a.Name.Data[i], b.Name.Data[i]
		if x >= 'A' && x <= 'Z' {
			x += 'a' - 'A'
		}
		if y >= 'A' && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}
