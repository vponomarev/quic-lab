package approuting

import (
	"context"
	"encoding/binary"
	"errors"
	"net/netip"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var ownerIPHelper = windows.NewLazySystemDLL("iphlpapi.dll")
var ownerTCPTable = ownerIPHelper.NewProc("GetExtendedTcpTable")
var ownerUDPTable = ownerIPHelper.NewProc("GetExtendedUdpTable")

const maxOwnerTable = 4 << 20

// Native calls cannot be canceled by context. Keep their bound across every
// engine generation, including reconnects while old system calls remain stuck.
var windowsOwnerResolver = NewBoundedResolver(windowsAttribution, 4, 150*time.Millisecond)

func NewWindowsOwnerResolver() *BoundedResolver {
	return windowsOwnerResolver
}

func windowsAttribution(ctx context.Context, f FlowKey) Attribution {
	fail := func(reason string) Attribution { return Attribution{Quality: Unknown, Reason: reason} }
	if f.Protocol != 6 && f.Protocol != 17 {
		return Attribution{Quality: Unsupported, Reason: "unsupported_protocol"}
	}
	if !f.Source.Addr().Is4() || !f.Destination.Addr().Is4() {
		return Attribution{Quality: Unsupported, Reason: "unsupported_address"}
	}
	var started windows.Filetime
	windows.GetSystemTimeAsFileTime(&started)
	before := uint64(started.HighDateTime)<<32 | uint64(started.LowDateTime)
	rows, e := readOwnerTable(f.Protocol)
	if e != nil {
		return fail("table_unavailable")
	}
	ids, reason := OwnerCandidates(f, rows)
	if reason != "" {
		return fail(reason)
	}
	a := Attribution{Quality: Resolved}
	if len(ids) > 1 {
		a.Quality = Shared
	}
	for _, pid := range ids {
		if ctx.Err() != nil {
			return fail("canceled")
		}
		identity, e := processIdentity(pid, before)
		if e != nil {
			return fail("process_unavailable")
		}
		a.Owners = append(a.Owners, identity)
	}
	return a
}

func readOwnerTable(protocol uint8) ([]OwnerRow, error) {
	proc, class := ownerTCPTable, uintptr(5) // TCP_TABLE_OWNER_PID_ALL
	if protocol == 17 {
		proc, class = ownerUDPTable, 1
	} // UDP_TABLE_OWNER_PID
	size := uint32(16384)
	for attempt := 0; attempt < 3; attempt++ {
		if size < 4 || size > maxOwnerTable {
			return nil, ErrExecutable
		}
		b := make([]byte, size)
		ret, _, _ := proc.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&size)), 0, windows.AF_INET, class, 0)
		runtime.KeepAlive(b)
		if ret == uintptr(windows.ERROR_INSUFFICIENT_BUFFER) {
			continue
		}
		if ret != 0 || size > uint32(len(b)) {
			return nil, ErrExecutable
		}
		return decodeOwnerTable(b[:size], protocol)
	}
	return nil, ErrExecutable
}

// OWNER_PID IPv4 rows contain DWORDs only (4 byte alignment). Address/port
// bytes are in network order; count and PID are native little-endian DWORDs.
func decodeOwnerTable(b []byte, protocol uint8) ([]OwnerRow, error) {
	stride := 24
	if protocol == 17 {
		stride = 12
	} else if protocol != 6 {
		return nil, ErrExecutable
	}
	if len(b) < 4 || len(b) > maxOwnerTable {
		return nil, ErrExecutable
	}
	count := uint64(binary.LittleEndian.Uint32(b))
	if count > uint64((len(b)-4)/stride) {
		return nil, ErrExecutable
	}
	rows := make([]OwnerRow, 0, int(count))
	endpoint := func(b []byte) netip.AddrPort {
		return netip.AddrPortFrom(netip.AddrFrom4([4]byte{b[0], b[1], b[2], b[3]}), binary.BigEndian.Uint16(b[4:6]))
	}
	for i := 0; i < int(count); i++ {
		r := b[4+i*stride : 4+(i+1)*stride]
		if protocol == 6 {
			rows = append(rows, OwnerRow{Local: endpoint(r[4:]), Remote: endpoint(r[12:]), PID: binary.LittleEndian.Uint32(r[20:])})
		} else {
			rows = append(rows, OwnerRow{Local: endpoint(r), PID: binary.LittleEndian.Uint32(r[8:])})
		}
	}
	return rows, nil
}

func processIdentity(pid uint32, before uint64) (ProcessIdentity, error) {
	h, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if e != nil {
		return ProcessIdentity{}, e
	}
	defer windows.CloseHandle(h)
	var created, exited, kernel, user windows.Filetime
	if e = windows.GetProcessTimes(h, &created, &exited, &kernel, &user); e != nil {
		return ProcessIdentity{}, e
	}
	stamp := uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	if stamp == 0 || stamp > before {
		return ProcessIdentity{}, errors.New("process changed after table snapshot")
	}
	buf := make([]uint16, 32768)
	n := uint32(len(buf))
	if e = windows.QueryFullProcessImageName(h, 0, &buf[0], &n); e != nil {
		return ProcessIdentity{}, e
	}
	identity, e := ResolveExecutable(windows.UTF16ToString(buf[:n]))
	if e != nil {
		return ProcessIdentity{}, e
	}
	var code uint32
	if e = windows.GetExitCodeProcess(h, &code); e != nil || code != 259 {
		return ProcessIdentity{}, ErrExecutable
	}
	return ProcessIdentity{Path: identity.Path, PID: pid, Created: stamp}, nil
}

// Revalidate both remembered lists before activation. A disabled stale list
// remains editable; canonical aliases cannot match twice in an active policy.
func CanonicalTUNPolicy(p *TUNPolicy) (*TUNPolicy, error) {
	if p == nil {
		return nil, nil
	}
	c := p.Clone()
	if _, e := CompileTUN(*c); e != nil {
		return nil, e
	}
	if !c.Enabled {
		return c, nil
	}
	for _, list := range [][]string{c.Only, c.Except} {
		for i, path := range list {
			id, e := ResolveExecutable(path)
			if e != nil {
				return nil, ErrPolicy
			}
			list[i] = id.Path
		}
	}
	if _, e := CompileTUN(*c); e != nil {
		return nil, e
	}
	return c, nil
}
