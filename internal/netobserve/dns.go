// Package netobserve decodes bounded network observations without collecting or exporting them.
package netobserve

import (
	"encoding/binary"
	"errors"
	"golang.org/x/net/dns/dnsmessage"
	"net"
	"strings"
)

type DNSQuestion struct {
	Name        string
	Type, Class uint16
}
type DNSAnswer struct {
	Name           string
	Type, Class    uint16
	TTL            uint32
	Address, CNAME string
}
type DNSMessage struct {
	ID        uint16
	Response  bool
	RCode     uint16
	Truncated bool
	Questions []DNSQuestion
	Answers   []DNSAnswer
}

// DecodeDNS accepts one complete DNS message, not TCP framing or encrypted DNS.
// It has no consent, storage, routing, or network side effects.
func DecodeDNS(packet []byte) (DNSMessage, error) {
	var out DNSMessage
	if len(packet) < 12 || len(packet) > 4096 {
		return out, errors.New("DNS message size exceeds bounds")
	}
	if binary.BigEndian.Uint16(packet[4:6]) > 8 || binary.BigEndian.Uint16(packet[6:8]) > 32 || binary.BigEndian.Uint16(packet[8:10]) > 32 || binary.BigEndian.Uint16(packet[10:12]) > 32 {
		return out, errors.New("DNS section counts exceed bounds")
	}
	var m dnsmessage.Message
	if e := m.Unpack(packet); e != nil {
		return out, e
	}
	out.ID = m.ID
	out.Response = m.Response
	out.RCode = uint16(m.RCode)
	out.Truncated = m.Truncated
	for _, q := range m.Questions {
		out.Questions = append(out.Questions, DNSQuestion{q.Name.String(), uint16(q.Type), uint16(q.Class)})
	}
	for _, r := range m.Answers {
		a := DNSAnswer{Name: r.Header.Name.String(), Type: uint16(r.Header.Type), Class: uint16(r.Header.Class), TTL: r.Header.TTL}
		switch b := r.Body.(type) {
		case *dnsmessage.AResource:
			a.Address = net.IP(b.A[:]).String()
		case *dnsmessage.AAAAResource:
			a.Address = net.IP(b.AAAA[:]).String()
		case *dnsmessage.CNAMEResource:
			a.CNAME = b.CNAME.String()
		default:
			continue
		}
		out.Answers = append(out.Answers, a)
	}
	return out, nil
}

// Fields preserves the research JSON schema; class is an additive DNS fact.
func (m DNSMessage) Fields() map[string]any {
	q := make([]map[string]any, 0, len(m.Questions))
	for _, v := range m.Questions {
		q = append(q, map[string]any{"name": v.Name, "qtype": v.Type, "class": v.Class})
	}
	a := make([]map[string]any, 0, len(m.Answers))
	for _, v := range m.Answers {
		r := map[string]any{"name": v.Name, "type": v.Type, "class": v.Class, "ttl": v.TTL}
		if v.Address != "" {
			r["address"] = v.Address
		}
		if v.CNAME != "" {
			r["cname"] = v.CNAME
		}
		a = append(a, r)
	}
	out := map[string]any{"transaction_id": m.ID, "response": m.Response, "rcode": m.RCode, "truncated": m.Truncated, "questions": q, "answers": a}
	if len(m.Questions) > 0 {
		out["question"] = m.Questions[0].Name
		out["domain"] = strings.TrimSuffix(m.Questions[0].Name, ".")
		out["qtype"] = m.Questions[0].Type
	}
	return out
}
