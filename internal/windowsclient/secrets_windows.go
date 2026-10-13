package windowsclient

import (
	"errors"
	"golang.org/x/sys/windows"
	"runtime"
	"unsafe"
)

var ErrSecrets = errors.New("protected_config_unavailable")

func Protect(b []byte) ([]byte, error)   { return crypt(b, true) }
func Unprotect(b []byte) ([]byte, error) { return crypt(b, false) }
func crypt(b []byte, encrypt bool) ([]byte, error) {
	if len(b) == 0 || len(b) > MaxFrame+65536 {
		return nil, ErrSecrets
	}
	in := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	var out windows.DataBlob
	var e error
	if encrypt {
		e = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		e = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	runtime.KeepAlive(b)
	if e != nil {
		return nil, ErrSecrets
	}
	defer windows.LocalFree(windows.Handle(uintptr(unsafe.Pointer(out.Data))))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}
