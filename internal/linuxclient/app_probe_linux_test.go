//go:build linux && !android

package linuxclient

import (
	"bytes"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Invoked only by test-linux-per-app.sh, from two distinct executable files.
func TestLinuxPerAppProbe(t *testing.T) {
	expected := os.Getenv("QUIC_APP_PROBE_EXPECT")
	if expected == "" {
		t.Skip("isolated network namespace fixture only")
	}
	if gate := os.Getenv("QUIC_APP_PROBE_GATE"); gate != "" {
		if err := os.WriteFile(gate+".ready", nil, 0600); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(gate); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("probe gate timeout")
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	probes := []struct{ network, address string }{{"tcp4", "192.0.2.2:9011"}, {"udp4", "192.0.2.2:9012"}}
	if os.Getenv("QUIC_APP_PROBE_DNS") == "1" {
		probes = probes[1:]
		probes[0].address = "192.0.2.2:53"
	}
	for _, probe := range probes {
		t.Run(probe.network, func(t *testing.T) {
			c, err := appProbeDial(probe.network, probe.address)
			if err != nil {
				if expected == "block" {
					return
				}
				t.Fatal(err)
			}
			defer c.Close()
			c.SetDeadline(time.Now().Add(2 * time.Second))
			payload := bytes.Repeat([]byte("per-app-payload"), 50)
			_, err = c.Write(payload)
			var response []byte
			if err == nil {
				b := make([]byte, 4096)
				for len(response) < len(payload)+len(expected)+1 {
					var n int
					n, err = c.Read(b)
					response = append(response, b[:n]...)
					if err != nil || probe.network == "udp4" {
						break
					}
				}
			}
			if expected == "block" {
				if len(response) != 0 || err == nil {
					t.Fatalf("blocked flow returned %d bytes: %v", len(response), err)
				}
				return
			}
			want := fmt.Sprintf("%s|%s", expected, payload)
			if string(response) != want {
				t.Fatalf("source/payload mismatch: expected source %s, received %s; error %v", expected, strings.SplitN(string(response), "|", 2)[0], err)
			}
		})
	}
}

func appProbeDial(network, address string) (net.Conn, error) {
	if os.Getenv("QUIC_APP_PROBE_DUALSTACK") != "1" {
		return net.DialTimeout(network, address, 2*time.Second)
	}
	kind := unix.SOCK_STREAM
	if network == "udp4" {
		kind = unix.SOCK_DGRAM
	}
	fd, err := unix.Socket(unix.AF_INET6, kind|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "dual-stack-probe")
	defer f.Close()
	if err := unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, 0); err != nil {
		return nil, err
	}
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_SNDTIMEO, &unix.Timeval{Sec: 2}); err != nil {
		return nil, err
	}
	endpoint := netip.MustParseAddrPort(address)
	if err := unix.Connect(fd, &unix.SockaddrInet6{Port: int(endpoint.Port()), Addr: endpoint.Addr().As16()}); err != nil {
		return nil, err
	}
	return net.FileConn(f)
}
