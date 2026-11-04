package serverexit

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

const brokerName = "server-exits.sock"

type Lease struct {
	Provider Provider
	Source   netip.Addr
	Gateway  netip.Addr // Native ingress address; its Echo measures the local hop.
	Release  func()
}
type Broker struct {
	// Only authenticated local workers use this endpoint. Acquire validates the
	// worker's ingress identity and atomically registers close against revocation.
	Acquire func(protocol, identity string, close func()) (Lease, error)
	Packets func(context.Context, Lease, net.Conn) error
}
type brokerRequest struct{ Protocol, Identity, Network, Address string }
type brokerReply struct{ OK bool }

func (b *Broker) Serve(ctx context.Context, dir string) (func(), error) {
	if b.Acquire == nil {
		return nil, ErrConfig
	}
	listener, cleanup, err := privateListener(dir, brokerName)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	var once sync.Once
	var wg sync.WaitGroup
	var mu sync.Mutex
	conns := map[net.Conn]bool{}
	slots := make(chan struct{}, 2048)
	done := make(chan struct{})
	stop := func() {
		once.Do(func() {
			cancel()
			listener.Close()
			<-done
			mu.Lock()
			for c := range conns {
				c.Close()
			}
			mu.Unlock()
			wg.Wait()
			cleanup()
		})
	}
	go func() {
		defer close(done)
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			select {
			case slots <- struct{}{}:
			default:
				c.Close()
				continue
			}
			mu.Lock()
			conns[c] = true
			mu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { c.Close(); mu.Lock(); delete(conns, c); mu.Unlock(); <-slots }()
				b.serve(ctx, c)
			}()
		}
	}()
	go func() { <-ctx.Done(); stop() }()
	return stop, nil
}
func (b *Broker) serve(parent context.Context, c net.Conn) {
	c.SetDeadline(time.Now().Add(10 * time.Second))
	raw, err := readFrame(c, 4096)
	if err != nil {
		return
	}
	var req brokerRequest
	if json.Unmarshal(raw, &req) != nil || len(req.Identity) > 128 || req.Identity == "" || (req.Protocol != "vless" && req.Protocol != "awg") {
		return
	}
	if req.Network != "tcp4" && req.Network != "udp4" && req.Network != "packet" {
		return
	}
	if req.Network == "packet" {
		if req.Protocol != "awg" || b.Packets == nil || req.Address != "" {
			return
		}
	} else {
		host, port, err := net.SplitHostPort(req.Address)
		n, e := strconv.ParseUint(port, 10, 16)
		if err != nil || e != nil || n == 0 || len(host) == 0 || len(host) > 253 {
			return
		}
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	lease, err := b.Acquire(req.Protocol, req.Identity, func() { cancel(); c.Close() })
	if err != nil {
		writeJSONFrame(c, brokerReply{})
		return
	}
	if lease.Release == nil || lease.Provider == nil {
		if lease.Release != nil {
			lease.Release()
		}
		return
	}
	defer lease.Release()
	if req.Network == "packet" {
		if !lease.Source.Is4() {
			return
		}
		if writeJSONFrame(c, brokerReply{OK: true}) != nil {
			return
		}
		c.SetDeadline(time.Time{})
		b.Packets(ctx, lease, &packetConn{Conn: c})
		return
	}
	dialCtx, stopDial := context.WithTimeout(ctx, 8*time.Second)
	out, err := lease.Provider.DialContext(dialCtx, req.Network, req.Address)
	stopDial()
	if err != nil {
		if out != nil {
			out.Close()
		}
		writeJSONFrame(c, brokerReply{})
		return
	}
	defer out.Close()
	if writeJSONFrame(c, brokerReply{OK: true}) != nil {
		return
	}
	c.SetDeadline(time.Time{})
	var incoming net.Conn = c
	if req.Network == "udp4" {
		incoming = &packetConn{Conn: c}
	}
	relay(ctx, incoming, out, req.Network == "udp4")
}

type Client struct{ Dir string }

func (c Client) Dial(ctx context.Context, protocol, identity, network, address string) (net.Conn, error) {
	if err := privateDirectory(c.Dir); err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(c.Dir, brokerName))
	if err != nil {
		return nil, ErrDenied
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	err = writeJSONFrame(conn, brokerRequest{protocol, identity, network, address})
	if err == nil {
		var raw []byte
		raw, err = readFrame(conn, 1024)
		var reply brokerReply
		if err == nil && (json.Unmarshal(raw, &reply) != nil || !reply.OK) {
			err = ErrDenied
		}
	}
	stop()
	if err != nil || ctx.Err() != nil {
		conn.Close()
		return nil, ErrDenied
	}
	conn.SetDeadline(time.Time{})
	if network == "udp4" || network == "packet" {
		return &packetConn{Conn: conn}, nil
	}
	return conn, nil
}
func writeJSONFrame(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return writeFrame(w, b)
}
func writeFrame(w io.Writer, b []byte) error {
	if len(b) > 65535 {
		return errors.New("packet too large")
	}
	var header [2]byte
	binary.BigEndian.PutUint16(header[:], uint16(len(b)))
	if _, err := io.Copy(w, bytes.NewReader(append(header[:], b...))); err != nil {
		return err
	}
	return nil
}

func readFrame(r io.Reader, max int) ([]byte, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(header[:]))
	if n > max {
		return nil, errors.New("frame too large")
	}
	b := make([]byte, n)
	_, err := io.ReadFull(r, b)
	return b, err
}

type packetConn struct {
	net.Conn
	readMu, writeMu sync.Mutex
}

func (c *packetConn) Read(b []byte) (int, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	p, err := readFrame(c.Conn, 65535)
	if err != nil {
		return 0, err
	}
	n := copy(b, p)
	if n < len(p) {
		return n, io.ErrShortBuffer
	}
	return n, nil
}
func (c *packetConn) Write(b []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := writeFrame(c.Conn, b); err != nil {
		return 0, err
	}
	return len(b), nil
}

func relay(ctx context.Context, a, b net.Conn, datagram bool) {
	stop := context.AfterFunc(ctx, func() { a.Close(); b.Close() })
	defer stop()
	defer a.Close()
	defer b.Close()
	done := make(chan struct{})
	copyFlow := func(dst, src net.Conn) {
		if !datagram {
			_, err := io.Copy(dst, src)
			if err != nil {
				a.Close()
				b.Close()
			} else if half, ok := dst.(interface{ CloseWrite() error }); ok {
				half.CloseWrite()
			} else {
				dst.Close()
			}
			return
		}
		defer a.Close()
		defer b.Close()
		buf := make([]byte, 65535)
		for {
			src.SetReadDeadline(time.Now().Add(90 * time.Second))
			n, err := src.Read(buf)
			if err != nil {
				return
			}
			dst.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, err = dst.Write(buf[:n]); err != nil {
				return
			}
		}
	}
	go func() { defer close(done); copyFlow(a, b) }()
	copyFlow(b, a)
	<-done
}
