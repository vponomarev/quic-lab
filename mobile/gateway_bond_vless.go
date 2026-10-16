package mobile

import (
	"context"
	"quiclab/internal/vless"
	"sync"
)

// Each joined relay path owns a separate engine. Its lifetime ends with the path,
// not with the short-lived handshake context or with another candidate.
func newBondVLESSDialer(raw, endpoint string, binder SocketBinder) (bondStreamDialer, func(), error) {
	config, err := decodeGatewayVLESS(raw, endpoint)
	if err != nil {
		return nil, nil, err
	}
	factory, err := newVLESSSocketFactory(endpoint, binder)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	engine, err := vless.New(ctx, config, factory)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	var once sync.Once
	cleanup := func() { once.Do(func() { cancel(); engine.Close() }) }
	return engine.DialContext, cleanup, nil
}
