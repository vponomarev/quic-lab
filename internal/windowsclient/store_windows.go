package windowsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"path/filepath"
	"quiclab/internal/vpnclient"
	"quiclab/internal/vpnmodel"
	"sync"
	"unsafe"
)

type EncryptedStore struct {
	dir string
	mu  sync.Mutex
}

func NewEncryptedStore(dir string) (*EncryptedStore, error) {
	u, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		return nil, ErrSecrets
	}
	if e = secureStoreDirectory(dir, u.User.Sid.String()); e != nil {
		return nil, e
	}
	return &EncryptedStore{dir: dir}, nil
}

// Create with a restrictive DACL atomically; never adopt another user's directory.
func secureStoreDirectory(dir string, principal string) error {
	if !filepath.IsAbs(dir) {
		return ErrSecrets
	}
	sid, e := windows.StringToSid(principal)
	if e != nil || sid.String() != principal {
		return ErrSecrets
	}
	sd, e := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;" + principal + ")")
	if e != nil {
		return ErrSecrets
	}
	path, e := windows.UTF16PtrFromString(dir)
	if e != nil {
		return ErrSecrets
	}
	sa := windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	e = windows.CreateDirectory(path, &sa)
	if e != nil && !errors.Is(e, windows.ERROR_ALREADY_EXISTS) {
		return ErrSecrets
	}
	// Parent paths are fixed by the installer; reject reparse components rather than following them.
	for current := filepath.Clean(dir); ; current = filepath.Dir(current) {
		info, e := os.Lstat(current)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrSecrets
		}
		next := filepath.Dir(current)
		if next == current {
			break
		}
	}
	actual, e := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if e != nil {
		return ErrSecrets
	}
	owner, _, e := actual.Owner()
	if e != nil {
		return ErrSecrets
	}
	trusted := owner.String() == principal || (principal == "S-1-5-18" && owner.String() == "S-1-5-32-544")
	if !trusted {
		return ErrSecrets
	}
	acl, _, e := sd.DACL()
	if e != nil {
		return ErrSecrets
	}
	if windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil) != nil {
		return ErrSecrets
	}
	return nil
}
func (s *EncryptedStore) Load(ctx context.Context) (*vpnclient.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	f, e := os.Open(filepath.Join(s.dir, "config.dpapi"))
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, ErrSecrets
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, MaxFrame+65537))
	if e != nil || len(b) > MaxFrame+65536 {
		return nil, ErrSecrets
	}
	b, e = Unprotect(b)
	if e != nil {
		return nil, e
	}
	defer clear(b)
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	var c vpnclient.Config
	var extra any
	if d.Decode(&c) != nil || d.Decode(&extra) != io.EOF || vpnmodel.Validate(c.Profiles) != nil {
		return nil, ErrSecrets
	}
	return &c, nil
}
func (s *EncryptedStore) Save(ctx context.Context, c vpnclient.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if vpnmodel.Validate(c.Profiles) != nil {
		return ErrSecrets
	}
	b, e := json.Marshal(c)
	if e != nil || len(b) > MaxFrame {
		return ErrSecrets
	}
	defer clear(b)
	encrypted, e := Protect(b)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(s.dir, "config-*.tmp")
	if e != nil {
		return ErrSecrets
	}
	defer os.Remove(f.Name())
	_, e = f.Write(encrypted)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return ErrSecrets
	}
	from, _ := windows.UTF16PtrFromString(f.Name())
	to, _ := windows.UTF16PtrFromString(filepath.Join(s.dir, "config.dpapi"))
	if windows.MoveFileEx(from, to, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH) != nil {
		return ErrSecrets
	}
	return nil
}
