package mobile

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"syscall"
	"time"
)

// Android resolves the endpoint with the selected Network before constructing
// this factory. Xray cannot invoke the system resolver or choose another endpoint.
type vlessSocketFactory struct {
	endpoint string
	binder   SocketBinder
}

func newVLESSSocketFactory(endpoint string, binder SocketBinder) (*vlessSocketFactory, error) {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return nil, errors.New("VLESS requires an IPv4 endpoint")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() {
		return nil, errors.New("VLESS endpoint must be resolved on its physical network")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || binder == nil {
		return nil, errors.New("VLESS endpoint and socket binder required")
	}
	return &vlessSocketFactory{endpoint: endpoint, binder: binder}, nil
}
func (f *vlessSocketFactory) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp4" || address != f.endpoint {
		return nil, errors.New("VLESS socket destination is outside its profile")
	}
	dialer := net.Dialer{Timeout: 5 * time.Second, Control: func(_, _ string, raw syscall.RawConn) error {
		var bindErr error
		if err := raw.Control(func(fd uintptr) { bindErr = f.binder.Bind(int64(fd)) }); err != nil {
			return err
		}
		return bindErr
	}}
	c, err := dialer.DialContext(ctx, "tcp4", f.endpoint)
	if err != nil {
		return nil, err
	}
	return meterBoundConn(c, f.binder), nil
}
