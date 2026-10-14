package approuting

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"golang.org/x/sys/windows"
	"path/filepath"
	"runtime"
	"strings"
	"unsafe"
)

type Identity struct {
	Path  string `json:"path"`
	ID    string `json:"id"`
	AppID []byte `json:"-"`
}

var ErrExecutable = errors.New("executable identity unavailable")
var fwpuclnt = windows.NewLazySystemDLL("fwpuclnt.dll")
var getAppID = fwpuclnt.NewProc("FwpmGetAppIdFromFileName0")
var freeWFP = fwpuclnt.NewProc("FwpmFreeMemory0")
var finalPath = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW")

type appBlob struct {
	Size uint32
	Data *byte
}

// ResolveExecutable is read-only and does not open a WFP engine or install a filter.
// IDs describe Windows executable paths, not file contents or a publisher trust decision.
func ResolveExecutable(path string) (Identity, error) {
	if len(path) > 32760 || len(path) < 3 || !filepath.IsAbs(path) || len(filepath.VolumeName(path)) != 2 || strings.Contains(path[2:], ":") || !strings.EqualFold(filepath.Ext(path), ".exe") {
		return Identity{}, ErrExecutable
	}
	p, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return Identity{}, ErrExecutable
	}
	h, e := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if e != nil {
		return Identity{}, ErrExecutable
	}
	defer windows.CloseHandle(h)
	var info windows.ByHandleFileInformation
	if windows.GetFileInformationByHandle(h, &info) != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 {
		return Identity{}, ErrExecutable
	}
	buf := make([]uint16, 32768)
	n, _, _ := finalPath.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	if n == 0 || n >= uintptr(len(buf)) {
		return Identity{}, ErrExecutable
	}
	canonical := strings.TrimPrefix(windows.UTF16ToString(buf[:n]), `\\?\`)
	if len(filepath.VolumeName(canonical)) != 2 || !strings.EqualFold(filepath.Ext(canonical), ".exe") {
		return Identity{}, ErrExecutable
	}
	cp, e := windows.UTF16PtrFromString(canonical)
	if e != nil {
		return Identity{}, ErrExecutable
	}
	var blob *appBlob
	ret, _, _ := getAppID.Call(uintptr(unsafe.Pointer(cp)), uintptr(unsafe.Pointer(&blob)))
	runtime.KeepAlive(cp)
	if ret != 0 || blob == nil {
		return Identity{}, ErrExecutable
	}
	defer freeWFP.Call(uintptr(unsafe.Pointer(&blob)))
	if blob.Size == 0 || blob.Size > 65536 || blob.Data == nil {
		return Identity{}, ErrExecutable
	}
	raw := append([]byte(nil), unsafe.Slice(blob.Data, int(blob.Size))...)
	hash := sha256.Sum256(raw)
	return Identity{canonical, hex.EncodeToString(hash[:]), raw}, nil
}
