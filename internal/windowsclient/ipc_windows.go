package windowsclient

import (
	"context"
	"errors"
	winio "github.com/Microsoft/go-winio"
	"golang.org/x/sys/windows"
	"net"
	"strings"
	"sync"
	"time"
)

var ErrIdentity = errors.New("untrusted_pipe_identity")

func Listen(name, owner string) (net.Listener, error) {
	sid, e := windows.StringToSid(owner)
	if e != nil || sid.String() != owner || !strings.HasPrefix(name, `\\.\pipe\quic-lab-`) {
		return nil, ErrIdentity
	}
	return winio.ListenPipe(name, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GRGW;;;" + owner + ")", InputBufferSize: 65536, OutputBufferSize: 65536})
}
func pipeHandle(c net.Conn) (windows.Handle, error) {
	f, ok := c.(interface{ Fd() uintptr })
	if !ok {
		return 0, ErrIdentity
	}
	return windows.Handle(f.Fd()), nil
}
func VerifyServer(c net.Conn, expectedPID uint32) error {
	h, e := pipeHandle(c)
	if e != nil || expectedPID == 0 {
		return ErrIdentity
	}
	var pid uint32
	if windows.GetNamedPipeServerProcessId(h, &pid) != nil || pid != expectedPID {
		return ErrIdentity
	}
	return nil
}
func verifyClient(c net.Conn, owner string) error {
	h, e := pipeHandle(c)
	if e != nil {
		return e
	}
	var pid uint32
	if windows.GetNamedPipeClientProcessId(h, &pid) != nil || pid == 0 {
		return ErrIdentity
	}
	p, e := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if e != nil {
		return ErrIdentity
	}
	defer windows.CloseHandle(p)
	var token windows.Token
	if windows.OpenProcessToken(p, windows.TOKEN_QUERY, &token) != nil {
		return ErrIdentity
	}
	defer token.Close()
	u, e := token.GetTokenUser()
	if e != nil {
		return ErrIdentity
	}
	s := u.User.Sid.String()
	if s != owner && s != "S-1-5-18" {
		return ErrIdentity
	}
	return nil
}

// One bounded request per connection. Reconnecting UI never owns the running VPN context.
func Serve(ctx context.Context, l net.Listener, owner string, h *Handler) error {
	var workers sync.WaitGroup
	slots := make(chan struct{}, 16)
	stop := context.AfterFunc(ctx, func() { l.Close() })
	defer stop()
	defer l.Close()
	defer workers.Wait()
	for {
		c, e := l.Accept()
		if e != nil {
			if ctx.Err() != nil {
				return nil
			}
			return e
		}
		select {
		case slots <- struct{}{}:
		default:
			c.Close()
			continue
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { <-slots }()
			defer c.Close()
			closeOnStop := context.AfterFunc(ctx, func() { c.Close() })
			defer closeOnStop()
			c.SetDeadline(time.Now().Add(30 * time.Second))
			if verifyClient(c, owner) != nil {
				return
			}
			q, e := ReadRequest(c)
			if e != nil {
				return
			}
			requestCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
			defer cancel()
			r := h.Handle(requestCtx, q)
			WriteFrame(c, r)
		}()
	}
}
