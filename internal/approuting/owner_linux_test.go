//go:build linux && !android

package approuting

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestProcSocketTableMatching(t *testing.T) {
	data := "  sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n" +
		"0: 0200000A:04D2 01010101:01BB 01 0:0 0:0 0 1000 0 111\n" +
		"1: 00000000:04D2 00000000:0000 07 0:0 0:0 0 1000 0 222\n" +
		"2: 0200000A:04D2 02020202:01BB 01 0:0 0:0 0 1000 0 333\n"
	f := FlowKey{6, netip.MustParseAddrPort("10.0.0.2:1234"), netip.MustParseAddrPort("1.1.1.1:443")}
	rows, e := parseProcSockets([]byte(data))
	if e != nil {
		t.Fatal(e)
	}
	inodes, reason := procCandidates(f, rows)
	if reason != "" || len(inodes) != 1 || !inodes[111] {
		t.Fatal(inodes, reason)
	}
	f.Protocol = 17
	inodes, reason = procCandidates(f, rows)
	if reason != "" || len(inodes) != 2 || !inodes[111] || !inodes[222] {
		t.Fatal(inodes, reason)
	}
	if _, e = parseProcSockets([]byte("header\nmalformed\n")); e == nil {
		t.Fatal("partial table accepted")
	}
}

func TestLinuxOwnerDifferentUser(t *testing.T) {
	if os.Getenv("QUIC_OWNER_CHILD") == "1" {
		ready := os.NewFile(3, "ready")
		release := os.NewFile(4, "release")
		defer ready.Close()
		defer release.Close()
		c, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		fmt.Fprintln(ready, c.LocalAddr().String())
		release.Read(make([]byte, 1))
		return
	}
	if os.Geteuid() != 0 {
		t.Skip("requires root with cross-user proc visibility")
	}
	ready, out, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer ready.Close()
	defer out.Close()
	in, release, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	defer release.Close()
	cmd := exec.Command("/proc/self/exe", "-test.run=^TestLinuxOwnerDifferentUser$", "-test.timeout=10s")
	cmd.Env = append(os.Environ(), "QUIC_OWNER_CHILD=1")
	cmd.ExtraFiles = []*os.File{out, in}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 65534, Gid: 65534}}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { release.Close(); cmd.Process.Kill(); cmd.Wait() }()
	out.Close()
	var address string
	if _, err := fmt.Fscanln(ready, &address); err != nil {
		t.Fatal(err)
	}
	flow := FlowKey{17, netip.MustParseAddrPort(address), netip.MustParseAddrPort("127.0.0.1:9999")}
	a := NewLinuxOwnerResolver().Resolve(context.Background(), flow)
	if a.Quality != Resolved || len(a.Owners) != 1 || a.Owners[0].PID != uint32(cmd.Process.Pid) {
		t.Fatal("cross-user owner", a)
	}
}

func procFixture(t *testing.T) (string, FlowKey) {
	t.Helper()
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "self", "net"), 0700)
	table := "sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode\n0: 0200000A:04D2 01010101:01BB 01 0:0 0:0 0 1000 0 111\n"
	os.WriteFile(filepath.Join(root, "self", "net", "tcp"), []byte(table), 0600)
	return root, FlowKey{6, netip.MustParseAddrPort("10.0.0.2:1234"), netip.MustParseAddrPort("1.1.1.1:443")}
}
func addProcOwner(t *testing.T, root string, pid int, exe string) {
	t.Helper()
	dir := filepath.Join(root, fmt.Sprint(pid))
	os.MkdirAll(filepath.Join(dir, "fd"), 0700)
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "S"
	fields[19] = "12345"
	os.WriteFile(filepath.Join(dir, "stat"), []byte(fmt.Sprintf("%d (a tricky ) name) %s", pid, strings.Join(fields, " "))), 0600)
	if e := os.Symlink(exe, filepath.Join(dir, "exe")); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("socket:[111]", filepath.Join(dir, "fd", "3")); e != nil {
		t.Fatal(e)
	}
}
func TestProcOwnershipCompleteSharedAndMissing(t *testing.T) {
	root, f := procFixture(t)
	exe, _ := os.Executable()
	addProcOwner(t, root, 123, exe)
	a := resolveProcOwner(context.Background(), root, f)
	if a.Quality != Resolved || len(a.Owners) != 1 || a.Owners[0].Created != 12345 {
		t.Fatal(a)
	}
	addProcOwner(t, root, 456, exe)
	a = resolveProcOwner(context.Background(), root, f)
	if a.Quality != Shared || len(a.Owners) != 2 {
		t.Fatal(a)
	}
	os.Remove(filepath.Join(root, "456", "exe"))
	a = resolveProcOwner(context.Background(), root, f)
	if a.Quality != Unknown {
		t.Fatal("partial owners accepted", a)
	}
}
func TestProcOwnershipRejectsMissingOwnerAndIncompleteScan(t *testing.T) {
	root, f := procFixture(t)
	exe, _ := os.Executable()
	addProcOwner(t, root, 123, exe)
	os.Remove(filepath.Join(root, "123", "fd", "3"))
	a := resolveProcOwner(context.Background(), root, f)
	if a.Quality != Unknown {
		t.Fatal("inode without owner", a)
	}
	if _, e := procStartTime([]byte("123 (bad) S 0")); e == nil {
		t.Fatal("truncated stat")
	}
	os.Remove(filepath.Join(root, "123", "fd"))
	os.WriteFile(filepath.Join(root, "123", "fd"), []byte("not a directory"), 0600)
	a = resolveProcOwner(context.Background(), root, f)
	if a.Quality != Unknown {
		t.Fatal("incomplete scan", a)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a = resolveProcOwner(ctx, root, f)
	if a.Quality != Unknown || a.Reason != "canceled" {
		t.Fatal(a)
	}
}
func TestLinuxOwnerLiveTCPAndUDP(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("full proc visibility requires root fixture")
	}
	ln, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	c, e := net.Dial("tcp4", ln.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	peer, e := ln.Accept()
	if e != nil {
		t.Fatal(e)
	}
	defer peer.Close()
	udp, e := net.ListenPacket("udp4", "0.0.0.0:0")
	if e != nil {
		t.Fatal(e)
	}
	defer udp.Close()
	resolver := NewLinuxOwnerResolver()
	for _, f := range []FlowKey{{6, netip.MustParseAddrPort(c.LocalAddr().String()), netip.MustParseAddrPort(c.RemoteAddr().String())}, {17, netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(udp.LocalAddr().(*net.UDPAddr).Port)), netip.MustParseAddrPort("127.0.0.1:9999")}} {
		a := resolver.Resolve(context.Background(), f)
		if a.Quality != Resolved || len(a.Owners) != 1 || a.Owners[0].PID != uint32(os.Getpid()) {
			t.Fatal(a)
		}
	}
}

func TestLinuxProcResourceBounds(t *testing.T) {
	if _, err := parseProcSockets(make([]byte, maxProcTable+1)); err == nil {
		t.Fatal("oversized table accepted")
	}
	root, flow := procFixture(t)
	exe, _ := os.Executable()
	addProcOwner(t, root, 123, exe)
	fds := maxProcFDs
	if _, _, reason := inspectProcOwner(context.Background(), filepath.Join(root, "123"), 123, map[uint64]bool{111: true}, &fds); reason != "fd_limit" {
		t.Fatal("FD budget not enforced", reason)
	}
	var rows []procSocket
	for i := 1; i <= 17; i++ {
		rows = append(rows, procSocket{flow.Source, flow.Destination, uint64(i)})
	}
	if _, reason := procCandidates(flow, rows); reason != "too_many_owners" {
		t.Fatal(reason)
	}
	for i := 1; i <= 17; i++ {
		addProcOwner(t, root, 1000+i, exe)
	}
	if a := resolveProcOwner(context.Background(), root, flow); a.Quality != Unknown || a.Reason != "too_many_owners" {
		t.Fatal(a)
	}
	// A new Client must not reset the process-wide native-call budget.
	first, second := NewLinuxOwnerResolver(), NewLinuxOwnerResolver()
	if first != second || cap(first.slots) != 4 {
		t.Fatal("native-call limit reset across client instances")
	}
}

func TestProcIdentityAnchoredToOpenedDirectory(t *testing.T) {
	root, _ := procFixture(t)
	exe, _ := os.Executable()
	addProcOwner(t, root, 123, exe)
	dir, err := os.Open(filepath.Join(root, "123"))
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := os.Rename(filepath.Join(root, "123"), filepath.Join(root, "old")); err != nil {
		t.Fatal(err)
	}
	addProcOwner(t, root, 123, "/missing/reused-pid")
	if path, err := procExecutable(dir); err != nil || path != exe {
		t.Fatal("identity switched to reused path", path, err)
	}
}

func TestProcIPv4MappedSocketOwnersAreNotOmitted(t *testing.T) {
	for _, protocol := range []uint8{6, 17} {
		t.Run(fmt.Sprint(protocol), func(t *testing.T) {
			root, flow := procFixture(t)
			flow.Protocol = protocol
			table := "tcp"
			local6, remote6 := "0000000000000000FFFF00000200000A:04D2", "0000000000000000FFFF000001010101:01BB"
			if protocol == 17 {
				table = "udp"
				data, _ := os.ReadFile(filepath.Join(root, "self", "net", "tcp"))
				os.WriteFile(filepath.Join(root, "self", "net", table), data, 0600)
				local6, remote6 = "00000000000000000000000000000000:04D2", "00000000000000000000000000000000:0000"
			}
			raw := fmt.Sprintf("header\n0: %s %s 01 0:0 0:0 0 1000 0 222\n", local6, remote6)
			os.WriteFile(filepath.Join(root, "self", "net", table+"6"), []byte(raw), 0600)
			exe, _ := os.Executable()
			addProcOwner(t, root, 123, exe)
			if a := resolveProcOwner(context.Background(), root, flow); a.Quality != Unknown {
				t.Fatal("IPv6-table candidate omitted from owner coverage", a)
			}
			addProcOwner(t, root, 456, exe)
			fd := filepath.Join(root, "456", "fd", "3")
			os.Remove(fd)
			os.Symlink("socket:[222]", fd)
			if a := resolveProcOwner(context.Background(), root, flow); a.Quality != Shared || len(a.Owners) != 2 {
				t.Fatal("dual-stack owner omitted", a)
			}
		})
	}
}

func TestLinuxOwnerLiveIPv4OnDualStackSockets(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root proc visibility")
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	for _, kind := range []int{unix.SOCK_STREAM, unix.SOCK_DGRAM} {
		fd, err := unix.Socket(unix.AF_INET6, kind|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer unix.Close(fd)
		if err := unix.SetsockoptInt(fd, unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, 0); err != nil {
			t.Fatal(err)
		}
		port := ln.Addr().(*net.TCPAddr).Port
		remote := &unix.SockaddrInet6{Port: port, Addr: [16]byte{10: 0xff, 11: 0xff, 12: 127, 15: 1}}
		protocol := uint8(6)
		if kind == unix.SOCK_STREAM {
			if err := unix.Connect(fd, remote); err != nil {
				t.Fatal(err)
			}
			c, err := ln.Accept()
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
		} else {
			protocol = 17
			if err := unix.Bind(fd, &unix.SockaddrInet6{}); err != nil {
				t.Fatal(err)
			}
		}
		local, err := unix.Getsockname(fd)
		if err != nil {
			t.Fatal(err)
		}
		flow := FlowKey{protocol, netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(local.(*unix.SockaddrInet6).Port)), netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), uint16(port))}
		a := NewLinuxOwnerResolver().Resolve(context.Background(), flow)
		if a.Quality != Resolved || len(a.Owners) != 1 || a.Owners[0].PID != uint32(os.Getpid()) {
			t.Fatalf("IPv4 on dual-stack protocol %d: %+v", protocol, a)
		}
	}
}

func TestProcMappedUDPConnectedAndWildcardCandidates(t *testing.T) {
	flow := FlowKey{17, netip.MustParseAddrPort("10.0.0.2:1234"), netip.MustParseAddrPort("1.1.1.1:443")}
	rows, err := parseProcSockets([]byte("header\n" +
		"0: 0000000000000000FFFF00000200000A:04D2 0000000000000000FFFF000001010101:01BB 01 0:0 0:0 0 1000 0 111\n" +
		"1: 00000000000000000000000000000000:04D2 00000000000000000000000000000000:0000 07 0:0 0:0 0 1000 0 222\n" +
		"2: 0000000000000000FFFF00000200000A:04D2 0000000000000000FFFF000002020202:01BB 01 0:0 0:0 0 1000 0 333\n"))
	if err != nil {
		t.Fatal(err)
	}
	inodes, reason := procCandidates(flow, rows)
	if reason != "" || len(inodes) != 2 || !inodes[111] || !inodes[222] {
		t.Fatal(inodes, reason)
	}
}

func TestProcIPv6TableUnavailableIsNotPartialSuccess(t *testing.T) {
	root, flow := procFixture(t)
	exe, _ := os.Executable()
	addProcOwner(t, root, 123, exe)
	if err := os.Mkdir(filepath.Join(root, "self", "net", "tcp6"), 0700); err != nil {
		t.Fatal(err)
	}
	if a := resolveProcOwner(context.Background(), root, flow); a.Quality != Unknown || a.Reason != "table_unavailable" {
		t.Fatal("inaccessible second table accepted", a)
	}
}
