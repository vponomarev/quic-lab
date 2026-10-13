package windowsclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	winio "github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"os"
	"quiclab/internal/vpnclient"
	"testing"
	"time"
)

func owner(t *testing.T) string {
	u, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		t.Fatal(e)
	}
	return u.User.Sid.String()
}
func TestDPAPIProtectAndCorruption(t *testing.T) {
	plain := []byte("secret-fixture")
	encrypted, e := Protect(plain)
	if e != nil || bytes.Contains(encrypted, plain) {
		t.Fatal("protection failed", e)
	}
	got, e := Unprotect(encrypted)
	if e != nil || !bytes.Equal(got, plain) {
		t.Fatal("roundtrip", e)
	}
	encrypted[len(encrypted)/2] ^= 0xff
	if _, e = Unprotect(encrypted); e == nil {
		t.Fatal("corrupt data accepted")
	}
}
func TestNativePipeAuthorizationAndIdentity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	name := fmt.Sprintf(`\\.\pipe\quic-lab-test-%d`, os.Getpid())
	h := NewHandler(vpnclient.NewController(&testBackend{}), &memoryStore{})
	l, e := Listen(name, owner(t))
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, l, owner(t), h) }()
	c, e := winio.DialPipeContext(ctx, name)
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	if e = VerifyServer(c, uint32(os.Getpid())); e != nil {
		t.Fatal(e)
	}
	if VerifyServer(c, 0) == nil {
		t.Fatal("accepted untrusted server")
	}
	c.SetDeadline(time.Now().Add(3 * time.Second))
	if e = WriteFrame(c, Request{Version: 1, ID: "native", Command: "status"}); e != nil {
		t.Fatal(e)
	}
	raw, e := ReadFrame(c)
	if e != nil {
		t.Fatal(e)
	}
	var r Response
	if json.Unmarshal(raw, &r) != nil || !r.OK || r.ID != "native" {
		t.Fatal(string(raw))
	}
	c.Close()
	if second, e := Listen(name, owner(t)); e == nil {
		second.Close()
		t.Fatal("duplicate pipe accepted")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("listener shutdown blocked")
	}
}
func TestNativePipeRejectsUnauthorizedUser(t *testing.T) {
	name := fmt.Sprintf(`\\.\pipe\quic-lab-denied-%d`, os.Getpid())
	l, e := Listen(name, "S-1-5-18")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, e := winio.DialPipeContext(ctx, name)
	if e == nil {
		c.Close()
		t.Fatal("ordinary owner connected to SYSTEM-only pipe")
	}
}

func TestEncryptedStoreRestartAndCorruption(t *testing.T) {
	dir := t.TempDir()
	s, e := NewEncryptedStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	if c, e := s.Load(ctx); e != nil || c != nil {
		t.Fatal(c, e)
	}
	c := config()
	if e = s.Save(ctx, c); e != nil {
		t.Fatal(e)
	}
	s, e = NewEncryptedStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.Load(ctx)
	if e != nil || got.Revision != c.Revision {
		t.Fatal(got, e)
	}
	raw, e := os.ReadFile(dir + "/config.dpapi")
	if e != nil || bytes.Contains(raw, []byte("example.org")) {
		t.Fatal("plaintext config", e)
	}
	os.WriteFile(dir+"/config.dpapi", []byte("broken"), 0600)
	if _, e = s.Load(ctx); e == nil {
		t.Fatal("corrupt store accepted")
	}
}

func TestStoreRejectsHostilePrecreatedOwner(t *testing.T) {
	dir := t.TempDir()
	defer secureStoreDirectory(dir, owner(t))
	if err := secureStoreDirectory(dir, "S-1-5-18"); err == nil {
		t.Fatal("SYSTEM store accepted directory owned by ordinary user")
	}
}

var serviceDenial = flag.Bool("service-denial", false, "explicit installed service foreign-account test")

func TestInstalledServiceRejectsForeignAccount(t *testing.T) {
	if !*serviceDenial {
		t.Skip("requires separate ordinary test account")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, e := winio.DialPipeContext(ctx, ServicePipe)
	if e == nil {
		c.Close()
		t.Fatal("foreign account connected to service pipe")
	}
	if !errors.Is(e, windows.ERROR_ACCESS_DENIED) {
		t.Fatal("expected access denied, got", e)
	}
}
