//go:build linux && !android

package approuting

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

const maxProcTable = 4 << 20
const maxProcProcesses = 4096
const maxProcFDs = 65536

var errProc = errors.New("incomplete process information")
var linuxOwnerResolver = NewBoundedResolver(func(ctx context.Context, f FlowKey) Attribution {
	if !procVisible() {
		return Attribution{Quality: Unknown, Reason: "proc_visibility"}
	}
	return resolveProcOwner(ctx, "/proc", f)
}, 4, 250*time.Millisecond)

func NewLinuxOwnerResolver() *BoundedResolver { return linuxOwnerResolver }

type procSocket struct {
	local, remote netip.AddrPort
	inode         uint64
}

func parseProcSockets(raw []byte) ([]procSocket, error) {
	if len(raw) > maxProcTable {
		return nil, errProc
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 65536)
	if !scanner.Scan() {
		return nil, errProc
	}
	var rows []procSocket
	endpoint := func(s string) (netip.AddrPort, error) {
		parts := strings.Split(s, ":")
		if len(parts) != 2 || (len(parts[0]) != 8 && len(parts[0]) != 32) || len(parts[1]) != 4 {
			return netip.AddrPort{}, errProc
		}
		// Both proc formats print native-endian 32-bit words. IPv4-mapped
		// AF_INET6 endpoints participate in the same captured IPv4 flows.
		var raw [16]byte
		for i := 0; i < len(parts[0])/8; i++ {
			word, e := strconv.ParseUint(parts[0][i*8:(i+1)*8], 16, 32)
			if e != nil {
				return netip.AddrPort{}, e
			}
			binary.NativeEndian.PutUint32(raw[i*4:(i+1)*4], uint32(word))
		}
		port, e := strconv.ParseUint(parts[1], 16, 16)
		if e != nil {
			return netip.AddrPort{}, e
		}
		ip := netip.AddrFrom16(raw).Unmap()
		if len(parts[0]) == 8 {
			ip = netip.AddrFrom4([4]byte(raw[:4]))
		}
		return netip.AddrPortFrom(ip, uint16(port)), nil
	}
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if len(fields) < 10 {
			return nil, errProc
		}
		local, e := endpoint(fields[1])
		if e != nil {
			return nil, e
		}
		remote, e := endpoint(fields[2])
		if e != nil {
			return nil, e
		}
		inode, e := strconv.ParseUint(fields[9], 10, 64)
		if e != nil {
			return nil, e
		}
		rows = append(rows, procSocket{local, remote, inode})
	}
	if scanner.Err() != nil {
		return nil, errProc
	}
	return rows, nil
}
func procCandidates(f FlowKey, rows []procSocket) (map[uint64]bool, string) {
	if f.Protocol != 6 && f.Protocol != 17 {
		return nil, "unsupported_protocol"
	}
	if !f.Source.Addr().Is4() || !f.Destination.Addr().Is4() {
		return nil, "unsupported_address"
	}
	result := map[uint64]bool{}
	for _, r := range rows {
		match := r.local == f.Source && r.remote == f.Destination
		if f.Protocol == 17 {
			match = r.local.Port() == f.Source.Port() && (r.local.Addr() == f.Source.Addr() || r.local.Addr().IsUnspecified()) && (r.remote == f.Destination || r.remote.Addr().IsUnspecified() && r.remote.Port() == 0)
		}
		if match {
			if r.inode == 0 {
				return nil, "owner_unavailable"
			}
			result[r.inode] = true
			if len(result) > 16 {
				return nil, "too_many_owners"
			}
		}
	}
	if len(result) == 0 {
		return nil, "not_found"
	}
	return result, ""
}
func limitedFile(path string, limit int) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	return limitedRead(f, limit)
}
func limitedRead(r io.Reader, limit int) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, int64(limit)+1))
	if e != nil || len(b) > limit {
		return nil, errProc
	}
	return b, nil
}
func readAt(dir *os.File, name string, limit int) ([]byte, error) {
	fd, e := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	return limitedRead(f, limit)
}
func linkAt(dir *os.File, name string) (string, error) {
	b := make([]byte, 4097)
	n, e := unix.Readlinkat(int(dir.Fd()), name, b)
	if e != nil || n >= len(b) {
		return "", errProc
	}
	return string(b[:n]), nil
}
func procStartTime(raw []byte) (uint64, error) {
	end := bytes.LastIndexByte(raw, ')')
	if end < 0 {
		return 0, errProc
	}
	fields := strings.Fields(string(raw[end+1:]))
	if len(fields) < 20 {
		return 0, errProc
	}
	stamp, e := strconv.ParseUint(fields[19], 10, 64)
	if e != nil || stamp == 0 {
		return 0, errProc
	}
	return stamp, nil
}
func procExecutable(dir *os.File) (string, error) {
	path, e := linkAt(dir, "exe")
	if e != nil {
		return "", e
	}
	if _, ok := linuxPathKey(path); !ok {
		return "", errProc
	}
	var actual, visible unix.Stat_t
	if unix.Fstatat(int(dir.Fd()), "exe", &actual, 0) != nil || unix.Stat(path, &visible) != nil || actual.Dev != visible.Dev || actual.Ino != visible.Ino {
		return "", errProc
	}
	canonical, e := filepath.EvalSymlinks(path)
	if e != nil {
		return "", errProc
	}
	if _, ok := linuxPathKey(canonical); !ok {
		return "", errProc
	}
	return canonical, nil
}

// Hidepid can omit entire processes: a successful directory scan is then not
// evidence of sole ownership. Keep attribution unknown in restricted procfs.
func procVisible() bool {
	b, e := limitedFile("/proc/self/mountinfo", 1<<20)
	if e != nil {
		return false
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 || fields[4] != "/proc" {
			continue
		}
		for _, field := range fields[5:] {
			for _, opt := range strings.Split(field, ",") {
				if strings.HasPrefix(opt, "hidepid=") && opt != "hidepid=0" && opt != "hidepid=off" {
					return false
				}
			}
		}
		return true
	}
	return false
}

func resolveProcOwner(ctx context.Context, root string, f FlowKey) Attribution {
	fail := func(reason string) Attribution { return Attribution{Quality: Unknown, Reason: reason} }
	if ctx.Err() != nil {
		return fail("canceled")
	}
	if f.Protocol != 6 && f.Protocol != 17 {
		return Attribution{Quality: Unsupported, Reason: "unsupported_protocol"}
	}
	if !f.Source.Addr().Is4() || !f.Destination.Addr().Is4() {
		return Attribution{Quality: Unsupported, Reason: "unsupported_address"}
	}
	table := "tcp"
	if f.Protocol == 17 {
		table = "udp"
	}
	snapshot := func() (map[uint64]bool, string) {
		var rows []procSocket
		for _, name := range []string{table, table + "6"} {
			raw, e := limitedFile(filepath.Join(root, "self", "net", name), maxProcTable)
			// Kernels without IPv6 need not expose tcp6/udp6. An inaccessible
			// existing table is incomplete coverage, never a partial success.
			if name == table+"6" && os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return nil, "table_unavailable"
			}
			parsed, e := parseProcSockets(raw)
			if e != nil {
				return nil, "table_unavailable"
			}
			rows = append(rows, parsed...)
		}
		return procCandidates(f, rows)
	}
	candidates, reason := snapshot()
	if reason != "" {
		return fail(reason)
	}
	processes, e := os.Open(root)
	if e != nil {
		return fail("proc_unavailable")
	}
	defer processes.Close()
	found := map[uint64]bool{}
	a := Attribution{Quality: Resolved}
	pids, fds, entries := 0, 0, 0
	for {
		batch, e := processes.ReadDir(128)
		if e != nil && e != io.EOF {
			return fail("incomplete_scan")
		}
		for _, entry := range batch {
			entries++
			if entries > maxProcProcesses+1024 {
				return fail("process_limit")
			}
			pid, e := strconv.ParseUint(entry.Name(), 10, 32)
			if e != nil || pid == 0 {
				continue
			}
			pids++
			if pids > maxProcProcesses {
				return fail("process_limit")
			}
			if ctx.Err() != nil {
				return fail("canceled")
			}
			owner, inodes, reason := inspectProcOwner(ctx, filepath.Join(root, entry.Name()), uint32(pid), candidates, &fds)
			if reason != "" {
				return fail(reason)
			}
			if owner == nil {
				continue
			}
			a.Owners = append(a.Owners, *owner)
			if len(a.Owners) > 16 {
				return fail("too_many_owners")
			}
			for inode := range inodes {
				found[inode] = true
			}
		}
		if e == io.EOF {
			break
		}
	}
	if len(found) != len(candidates) {
		return fail("owner_not_visible")
	}
	after, reason := snapshot()
	if reason != "" || len(after) != len(candidates) {
		return fail("socket_changed")
	}
	for inode := range candidates {
		if !after[inode] {
			return fail("socket_changed")
		}
	}
	if len(a.Owners) == 0 {
		return fail("owner_not_visible")
	}
	if len(a.Owners) > 1 {
		a.Quality = Shared
	}
	return a
}

func inspectProcOwner(ctx context.Context, path string, pid uint32, candidates map[uint64]bool, totalFDs *int) (*ProcessIdentity, map[uint64]bool, string) {
	dir, e := os.Open(path)
	if os.IsNotExist(e) {
		return nil, nil, ""
	}
	if e != nil {
		return nil, nil, "incomplete_scan"
	}
	defer dir.Close()
	fd, e := unix.Openat(int(dir.Fd()), "fd", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if errors.Is(e, unix.ENOENT) || errors.Is(e, unix.ESRCH) {
		return nil, nil, ""
	}
	if e != nil {
		return nil, nil, "incomplete_scan"
	}
	descriptors := os.NewFile(uintptr(fd), "fd")
	defer descriptors.Close()
	var matched []string
	found := map[uint64]bool{}
	for {
		entries, e := descriptors.ReadDir(128)
		if e != nil && e != io.EOF {
			return nil, nil, "incomplete_scan"
		}
		for _, entry := range entries {
			*totalFDs++
			if *totalFDs > maxProcFDs {
				return nil, nil, "fd_limit"
			}
			if ctx.Err() != nil {
				return nil, nil, "canceled"
			}
			target, err := linkAt(descriptors, entry.Name())
			if err != nil {
				// A concurrently closed fd is not an inaccessible fd. Check using fstatat,
				// since linkAt intentionally returns only a sanitized error.
				var st unix.Stat_t
				err = unix.Fstatat(int(descriptors.Fd()), entry.Name(), &st, unix.AT_SYMLINK_NOFOLLOW)
				if errors.Is(err, unix.ENOENT) || errors.Is(err, unix.ESRCH) {
					continue
				}
				return nil, nil, "incomplete_scan"
			}
			if strings.HasPrefix(target, "socket:[") && strings.HasSuffix(target, "]") {
				inode, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]"), 10, 64)
				if err == nil && candidates[inode] {
					found[inode] = true
					matched = append(matched, entry.Name())
				}
			}
		}
		if e == io.EOF {
			break
		}
	}
	if len(found) == 0 {
		return nil, nil, ""
	}
	before, e := readAt(dir, "stat", 8192)
	if e != nil {
		return nil, nil, "process_changed"
	}
	created, e := procStartTime(before)
	if e != nil {
		return nil, nil, "process_changed"
	}
	exe, e := procExecutable(dir)
	if e != nil {
		return nil, nil, "executable_unavailable"
	}
	// Recheck every matched descriptor after identity lookup. Anchored directory
	// FDs cannot silently switch to a newly reused PID.
	confirmed := map[uint64]bool{}
	for _, name := range matched {
		target, e := linkAt(descriptors, name)
		if e != nil {
			return nil, nil, "socket_changed"
		}
		inode, e := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(target, "socket:["), "]"), 10, 64)
		if e != nil || !found[inode] {
			return nil, nil, "socket_changed"
		}
		confirmed[inode] = true
	}
	if len(confirmed) != len(found) {
		return nil, nil, "socket_changed"
	}
	after, e := readAt(dir, "stat", 8192)
	if e != nil {
		return nil, nil, "process_changed"
	}
	stamp, e := procStartTime(after)
	next, e2 := procExecutable(dir)
	if e != nil || e2 != nil || created != stamp || exe != next {
		return nil, nil, "process_changed"
	}
	return &ProcessIdentity{Path: exe, PID: pid, Created: created}, found, ""
}

func CanonicalLinuxTUNPolicy(p *TUNPolicy) (*TUNPolicy, error) {
	if p == nil {
		return nil, nil
	}
	c := p.Clone()
	if _, e := CompileLinuxTUN(*c); e != nil {
		return nil, e
	}
	if !c.Enabled {
		return c, nil
	}
	for _, list := range [][]string{c.Only, c.Except} {
		for i, path := range list {
			canonical, e := filepath.EvalSymlinks(path)
			if e != nil {
				return nil, ErrPolicy
			}
			info, e := os.Stat(canonical)
			if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
				return nil, ErrPolicy
			}
			list[i] = canonical
		}
	}
	if _, e := CompileLinuxTUN(*c); e != nil {
		return nil, e
	}
	return c, nil
}
