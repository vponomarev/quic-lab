package mobile

import (
	"errors"
	"net"
	"net/netip"
	"quiclab/internal/awg"
	"strconv"
	"sync"
)

func (c gatewayConfig) hasUnderlay() bool {
	return c.UnderlayVLESSConfig != "" || c.UnderlayAWGConfig != ""
}

func validateAWGEndpoint(endpoint string) error {
	address, err := netip.ParseAddrPort(endpoint)
	if err != nil || !address.Addr().Is4() || address.Addr().IsUnspecified() || address.Addr().IsMulticast() || address.Port() == 0 {
		return errors.New("AWG requires a resolved valid IPv4 endpoint")
	}
	return nil
}

// Validation never creates an engine or consults system DNS. The returned
// identity is private runtime metadata, never an event or a public profile ID.
func (c gatewayConfig) validateUnderlayProfile() ([32]byte, error) {
	if c.UnderlayVLESSConfig != "" && c.UnderlayAWGConfig != "" {
		return [32]byte{}, errors.New("choose exactly one underlay profile")
	}
	if !c.hasUnderlay() {
		if c.UnderlayEndpoint != "" {
			return [32]byte{}, errors.New("underlay endpoint without a profile")
		}
		return [32]byte{}, nil
	}
	if c.Transport != "https" {
		return [32]byte{}, errors.New("relay requires HTTPS carrier")
	}
	host, port, err := net.SplitHostPort(c.UnderlayEndpoint)
	n, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || host == "" || n < 1 || n > 65535 {
		return [32]byte{}, errors.New("underlay profile endpoint required")
	}
	if c.UnderlayAWGConfig != "" {
		config, err := awg.Parse(c.UnderlayAWGConfig)
		if err != nil {
			return [32]byte{}, err
		}
		return config.PeerIdentity(), nil
	}
	_, err = decodeGatewayVLESS(c.UnderlayVLESSConfig, c.UnderlayEndpoint)
	return [32]byte{}, err
}

// Only the bound Network may resolve the physical endpoint. Original profiles
// remain unresolved; this validation runs after the per-network substitution.
func (c gatewayConfig) validateUnderlay() ([32]byte, error) {
	identity, err := c.validateUnderlayProfile()
	if err != nil || !c.hasUnderlay() {
		return identity, err
	}
	if c.UnderlayAWGConfig != "" {
		return identity, validateAWGEndpoint(c.UnderlayEndpoint)
	}
	_, err = newVLESSSocketFactory(c.UnderlayEndpoint, vlessEndpointValidator{})
	return identity, err
}

func newBondAWGDialer(raw, endpoint string, binder SocketBinder) (bondStreamDialer, func(), error) {
	config, err := awg.Parse(raw)
	if err != nil {
		return nil, nil, err
	}
	if err = validateAWGEndpoint(endpoint); err != nil {
		return nil, nil, err
	}
	if binder == nil {
		return nil, nil, errors.New("AWG underlay socket binder required")
	}
	engine, err := awg.Start(config, endpoint, binder)
	if err != nil {
		return nil, nil, err
	}
	var once sync.Once
	cleanup := func() { once.Do(func() { engine.Close() }) }
	return engine.DialContext, cleanup, nil
}
