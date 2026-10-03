package mobile

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// StartExitEcho measures the complete selected VPN path to a user supplied
// host:port TCP responder. The responder must echo each exact 32-byte payload
// (QLEXIT1 followed by a random nonce). No public responder is built in.
// The interval is bounded to 100..60000 ms; each exchange has a 5 s deadline.
func (g *Gateway) StartExitEcho(target string, intervalMillis int64) error {
	target = strings.TrimSpace(target)
	host, port, err := net.SplitHostPort(target)
	p, portErr := strconv.Atoi(port)
	if err != nil || host == "" || strings.ContainsAny(host, " /\\\t\r\n") || portErr != nil || p < 1 || p > 65535 {
		return errors.New("echo target must be host:port")
	}
	if intervalMillis < 100 || intervalMillis > 60000 {
		return errors.New("echo interval must be 100..60000 ms")
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cancel == nil || g.ctx == nil || g.ctx.Err() != nil {
		return errors.New("gateway disconnected")
	}
	if g.direct != nil {
		return errors.New("exit echo requires a VPN gateway")
	}
	if g.exitEchoProbeCancel != nil {
		g.exitEchoProbeCancel()
	}
	g.exitEchoGeneration++
	g.exitEchoTarget, g.exitEchoInterval = target, time.Duration(intervalMillis)*time.Millisecond
	if g.exitEchoParent != g.ctx {
		g.exitEchoParent = g.ctx
		g.exitEchoWake = make(chan struct{}, 1)
		go g.exitEchoLoop(g.ctx, g.exitEchoWake)
	}
	select {
	case g.exitEchoWake <- struct{}{}:
	default:
	}
	return nil
}

// StopExitEcho cancels the current exchange. The one worker remains dormant
// until another diagnostic starts or the owning Gateway context ends.
func (g *Gateway) StopExitEcho() {
	g.mu.Lock()
	g.stopExitEchoLocked()
	g.mu.Unlock()
}

// ExitEchoGeneration identifies the diagnostic just started, independently of
// transport/session generations. Hosts use it to reject late replacement results.
func (g *Gateway) ExitEchoGeneration() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return int64(g.exitEchoGeneration)
}
func (g *Gateway) stopExitEchoLocked() {
	g.exitEchoGeneration++
	g.exitEchoTarget = ""
	if g.exitEchoProbeCancel != nil {
		g.exitEchoProbeCancel()
		g.exitEchoProbeCancel = nil
	}
	if g.exitEchoWake != nil {
		select {
		case g.exitEchoWake <- struct{}{}:
		default:
		}
	}
}
func (g *Gateway) exitEchoLoop(parent context.Context, wake <-chan struct{}) {
	for {
		g.mu.Lock()
		select {
		case <-wake:
		default:
		}
		target, interval, generation := g.exitEchoTarget, g.exitEchoInterval, g.exitEchoGeneration
		if parent.Err() != nil || g.exitEchoParent != parent {
			g.mu.Unlock()
			return
		}
		if target == "" {
			g.mu.Unlock()
			select {
			case <-parent.Done():
				return
			case <-wake:
				continue
			}
		}
		ctx, cancel := context.WithTimeout(parent, 5*time.Second)
		g.exitEchoProbeCancel = cancel
		g.mu.Unlock()
		started := time.Now()
		err := exitEchoProbe(ctx, func(ctx context.Context) (net.Conn, error) {
			st, err := g.dialExitEcho(ctx, "tcp", target)
			if err != nil {
				return nil, err
			}
			return diagnosticConn{st}, nil
		})
		elapsed := time.Since(started)
		cancel()
		g.mu.Lock()
		current := parent.Err() == nil && g.exitEchoParent == parent && g.exitEchoGeneration == generation && g.exitEchoTarget == target
		if current {
			g.exitEchoProbeCancel = nil
		}
		g.mu.Unlock()
		if current {
			result := map[string]any{"echo_generation": generation, "target": target}
			if err != nil {
				result["error"] = err.Error()
			} else {
				result["rtt_ms"] = float64(elapsed) / float64(time.Millisecond)
			}
			g.emit("exit_echo", result)
		}
		timer := time.NewTimer(interval)
		select {
		case <-parent.Done():
			timer.Stop()
			return
		case <-wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}
func exitEchoProbe(ctx context.Context, dial func(context.Context) (net.Conn, error)) error {
	c, err := dial(ctx)
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
	payload := make([]byte, 32)
	copy(payload, "QLEXIT1")
	if _, err = rand.Read(payload[7:]); err != nil {
		return err
	}
	if _, err = io.Copy(c, bytes.NewReader(payload)); err != nil {
		return err
	}
	reply := make([]byte, len(payload))
	if _, err = io.ReadFull(c, reply); err != nil {
		return err
	}
	if !bytes.Equal(payload, reply) {
		return errors.New("invalid exit echo reply")
	}
	return nil
}
