package admin

import (
	"net"
	"net/url"
)

type entryPoint struct{ Service, Network, Address, Binding string }

func (w *Web) entryPoints() []entryPoint {
	u, _ := url.Parse(w.Config.PublicURL)
	port := u.Port()
	if port == "" {
		port = "443"
	}
	public := net.JoinHostPort(u.Hostname(), port)
	binding := w.ListenerBindings["public"]
	adminBinding := w.Config.Listen
	echoBinding := w.ListenerBindings["echo-https"]
	if binding != "" {
		adminBinding = binding + " (HTTPS); " + w.Config.Listen + " (HTTP)"
		echoBinding = binding
	} else {
		adminBinding += " (HTTP за reverse proxy)"
		if echoBinding != "" {
			echoBinding += " (HTTP за reverse proxy)"
		}
	}
	entries := []entryPoint{{"Админка", "TCP / HTTPS", public + u.Path, adminBinding}, {"Echo QUIC", "UDP", w.Config.Echo.Endpoint, w.ListenerBindings["echo-quic"]}, {"Echo WebSocket", "TCP / HTTPS", net.JoinHostPort(w.Config.Echo.Hostname, port) + "/echo", echoBinding}, {"VPN QUIC · mTLS", "UDP", w.Config.VPN.QUIC, w.ListenerBindings["vpn-quic"]}, {"VPN WebSocket · mTLS", "TCP / HTTPS", w.Config.VPN.HTTPS + "/tunnel", w.ListenerBindings["vpn-https"]}}
	if w.Config.AWG != nil {
		_, p, _ := net.SplitHostPort(w.Config.AWG.Endpoint)
		entries = append(entries, entryPoint{"AmneziaWG", "UDP", w.Config.AWG.Endpoint, ":" + p + " · " + w.Config.AWG.Interface})
	}
	return entries
}
