package gateway

import (
	"context"
	"encoding/json"
	"github.com/coder/websocket"
	"net/http"
	"quiclab/internal/bondhttps"
	"time"
)

func (s *Server) ServeBondHTTPS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/tunnel/bond" {
		http.NotFound(w, r)
		return
	}
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
		http.Error(w, "client certificate required", http.StatusForbidden)
		return
	}
	c, e := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{bondhttps.Subprotocol}})
	if e != nil {
		return
	}
	defer c.CloseNow()
	if c.Subprotocol() != bondhttps.Subprotocol {
		return
	}
	if s.Register != nil || s.RegisterProtocol != nil {
		release, err := s.register(*r.TLS, "https", func() { c.CloseNow() })
		if err != nil {
			return
		}
		defer release()
	}
	c.SetReadLimit(4096)
	handshake, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	kind, raw, e := c.Read(handshake)
	if e != nil || kind != websocket.MessageText {
		return
	}
	var h BondHello
	if json.Unmarshal(raw, &h) != nil {
		return
	}
	root := s.BondContext
	if root == nil {
		root = context.Background()
	}
	entry, token, e := s.joinBond(root, handshake, *r.TLS, h, "https", r.RemoteAddr)
	reply := BondWelcome{Token: token}
	if e != nil {
		reply = BondWelcome{Error: e.Error()}
	}
	raw, _ = json.Marshal(reply)
	if c.Write(handshake, websocket.MessageText, raw) != nil || e != nil {
		return
	}
	pathCtx, stop := context.WithCancel(r.Context())
	defer stop()
	path := bondhttps.NewPath(c, stop)
	defer path.Close()
	if attachBond(entry, h, path) != nil {
		return
	}
	select {
	case <-pathCtx.Done():
	case <-entry.mux.Context().Done():
	}
}
