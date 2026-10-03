package vless

import (
	"context"
	"errors"
	"github.com/xtls/xray-core/common/protocol"
	"github.com/xtls/xray-core/common/serial"
	account "github.com/xtls/xray-core/proxy/vless"
	"strings"
)

func (s *Server) revokeLocked(id string) {
	s.gate.Revoke(id)
	// The gate is authoritative even if upstream removal returns an error.
	if _, ok := s.clients[id]; ok {
		s.users.RemoveUser(context.Background(), id)
		delete(s.clients, id)
	}
}

// SetClients changes accounts without restarting unrelated connections. Removed
// UUIDs are tombstoned for this server's lifetime and cannot be re-enabled.
func (s *Server) SetClients(ctx context.Context, clients []ServerClient) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.engine.ctx.Err(); e != nil {
		return e
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	if len(clients) > MaxServerClients {
		return errors.New("too many VLESS devices")
	}
	desired := map[string]string{}
	for _, c := range clients {
		id := strings.ToLower(c.UUID)
		if !validUUID(id) || (c.Flow != "" && c.Flow != "xtls-rprx-vision") {
			return errors.New("invalid VLESS account")
		}
		if _, ok := desired[id]; ok {
			return errors.New("duplicate VLESS account")
		}
		if flow, ok := s.clients[id]; ok && flow != c.Flow {
			return errors.New("VLESS flow change requires restart")
		}
		s.gate.mu.Lock()
		revoked := s.gate.revoked[id]
		s.gate.mu.Unlock()
		if revoked {
			return ErrDeviceRevoked
		}
		desired[id] = c.Flow
	}
	for id := range s.clients {
		if _, ok := desired[id]; !ok {
			s.revokeLocked(id)
		}
	}
	for id, flow := range desired {
		if _, ok := s.clients[id]; ok {
			continue
		}
		user, e := (&protocol.User{Email: id, Account: serial.ToTypedMessage(&account.Account{Id: id, Encryption: "none", Flow: flow})}).ToMemoryUser()
		if e != nil {
			return errors.New("invalid VLESS account")
		}
		if e = s.users.AddUser(ctx, user); e != nil {
			return errors.New("VLESS account update failed")
		}
		s.clients[id] = flow
	}
	return nil
}
