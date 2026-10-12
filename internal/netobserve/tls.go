package netobserve

import (
	"encoding/binary"
	"fmt"
	"strings"
)

const MaxTLSHelloBytes = 32 << 10

// TLSInfo is passive ClientHello evidence, NOT negotiated TLS or HTTP request data.
// ECHOffered includes GREASE; observing the extension does not prove acceptance.
type TLSInfo struct {
	State           string   `json:"state"`
	ServerName      string   `json:"server_name,omitempty"`
	ECHOffered      bool     `json:"ech_offered,omitempty"`
	ALPN            []string `json:"alpn_offered,omitempty"`
	OfferedVersions []string `json:"versions_offered,omitempty"`
}

// TLSProbe consumes the ordered upstream byte stream. Callers serialize access.
// Memory/work are bounded; buffers are discarded once classified. No sockets or IO.
type TLSProbe struct {
	record, handshake []byte
	total, records    int
	done              bool
	info              TLSInfo
}

func NewTLSProbe(missedStart bool) *TLSProbe {
	p := &TLSProbe{info: TLSInfo{State: "incomplete"}}
	if missedStart {
		p.finish("not_observed")
	}
	return p
}
func (p *TLSProbe) finish(state string) {
	p.info.State = state
	p.done = true
	p.record = nil
	p.handshake = nil
}
func (p *TLSProbe) Info() *TLSInfo {
	if p == nil {
		return nil
	}
	v := p.info
	v.ALPN = append([]string(nil), v.ALPN...)
	v.OfferedVersions = append([]string(nil), v.OfferedVersions...)
	return &v
}
func (p *TLSProbe) Feed(b []byte) {
	if p == nil || p.done {
		return
	}
	for len(b) > 0 && !p.done {
		need := 5 - len(p.record)
		if need <= 0 {
			n := int(binary.BigEndian.Uint16(p.record[3:5]))
			if p.record[0] != 22 || p.record[1] != 3 {
				p.finish("not_tls")
				return
			}
			if n == 0 {
				p.finish("malformed")
				return
			}
			if n > 18432 || p.total+5+n > MaxTLSHelloBytes {
				p.finish("limit")
				return
			}
			need = 5 + n - len(p.record)
		}
		take := min(need, len(b))
		p.record = append(p.record, b[:take]...)
		b = b[take:]
		if len(p.record) < 5 {
			continue
		}
		n := int(binary.BigEndian.Uint16(p.record[3:5]))
		if p.record[0] != 22 || p.record[1] != 3 {
			p.finish("not_tls")
			return
		}
		if n > 18432 || p.total+5+n > MaxTLSHelloBytes {
			p.finish("limit")
			return
		}
		if n == 0 {
			p.finish("malformed")
			return
		}
		if len(p.record) < 5+n {
			continue
		}
		p.records++
		if p.records > 64 {
			p.finish("limit")
			return
		}
		p.total += len(p.record)
		p.handshake = append(p.handshake, p.record[5:]...)
		p.record = nil
		if len(p.handshake) < 4 {
			continue
		}
		if p.handshake[0] != 1 {
			p.finish("not_observed")
			return
		}
		size := int(p.handshake[1])<<16 | int(p.handshake[2])<<8 | int(p.handshake[3])
		if size+4 > MaxTLSHelloBytes {
			p.finish("limit")
			return
		}
		if len(p.handshake) < size+4 {
			continue
		}
		info, ok := parseClientHello(p.handshake[4 : 4+size])
		if !ok {
			p.finish("malformed")
			return
		}
		p.info = info
		p.finish("observed")
	}
}

type tlsCursor struct {
	b   []byte
	bad bool
}

func (c *tlsCursor) take(n int) []byte {
	if n < 0 || n > len(c.b) {
		c.bad = true
		c.b = nil
		return nil
	}
	v := c.b[:n]
	c.b = c.b[n:]
	return v
}
func (c *tlsCursor) u8() int {
	b := c.take(1)
	if b == nil {
		return 0
	}
	return int(b[0])
}
func (c *tlsCursor) u16() int {
	b := c.take(2)
	if b == nil {
		return 0
	}
	return int(binary.BigEndian.Uint16(b))
}
func (c *tlsCursor) v8() []byte  { return c.take(c.u8()) }
func (c *tlsCursor) v16() []byte { return c.take(c.u16()) }
func tlsVersion(v int) string {
	switch v {
	case 0x301:
		return "TLS 1.0"
	case 0x302:
		return "TLS 1.1"
	case 0x303:
		return "TLS 1.2"
	case 0x304:
		return "TLS 1.3"
	}
	return fmt.Sprintf("0x%04x", v)
}
func parseClientHello(b []byte) (TLSInfo, bool) {
	c := tlsCursor{b: b}
	legacy := c.u16()
	c.take(32)
	session := c.v8()
	suites := c.v16()
	compression := c.v8()
	v := TLSInfo{OfferedVersions: []string{tlsVersion(legacy)}}
	if c.bad || len(session) > 32 || len(suites) == 0 || len(suites)%2 != 0 || len(compression) == 0 {
		return TLSInfo{}, false
	}
	if len(c.b) == 0 {
		return v, true
	}
	ext := tlsCursor{b: c.v16()}
	if c.bad || len(c.b) != 0 {
		return TLSInfo{}, false
	}
	seen := map[int]bool{}
	for len(ext.b) > 0 && !ext.bad {
		typ := ext.u16()
		data := ext.v16()
		if ext.bad || seen[typ] {
			return TLSInfo{}, false
		}
		seen[typ] = true
		x := tlsCursor{b: data}
		switch typ {
		case 0:
			names := tlsCursor{b: x.v16()}
			if x.bad || len(x.b) != 0 {
				return TLSInfo{}, false
			}
			hasName := false
			for len(names.b) > 0 && !names.bad {
				k := names.u8()
				name := names.v16()
				if k == 0 {
					if hasName || len(name) == 0 || len(name) > 253 {
						return TLSInfo{}, false
					}
					hasName = true
					for _, ch := range name {
						if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '.' || ch == '-') {
							return TLSInfo{}, false
						}
					}
					v.ServerName = strings.ToLower(string(name))
				}
			}
			if names.bad {
				return TLSInfo{}, false
			}
		case 16:
			list := tlsCursor{b: x.v16()}
			if x.bad || len(x.b) != 0 {
				return TLSInfo{}, false
			}
			for len(list.b) > 0 && !list.bad {
				a := list.v8()
				if len(a) == 0 || len(v.ALPN) >= 32 {
					return TLSInfo{}, false
				}
				v.ALPN = append(v.ALPN, string(a))
			}
			if list.bad {
				return TLSInfo{}, false
			}
		case 43:
			versions := tlsCursor{b: x.v8()}
			if x.bad || len(x.b) != 0 || len(versions.b) == 0 || len(versions.b)%2 != 0 {
				return TLSInfo{}, false
			}
			v.OfferedVersions = nil
			for len(versions.b) > 0 {
				v.OfferedVersions = append(v.OfferedVersions, tlsVersion(versions.u16()))
			}
		case 0xfe0d:
			v.ECHOffered = true
		}
	}
	return v, !ext.bad
}

// Validate bounds metadata received from an untrusted research client.
func (i *TLSInfo) Validate() error {
	switch i.State {
	case "incomplete", "not_observed", "not_tls", "limit", "malformed", "observed":
	default:
		return fmt.Errorf("invalid TLS state")
	}
	if len(i.ServerName) > 253 || len(i.ALPN) > 32 || len(i.OfferedVersions) > 127 {
		return fmt.Errorf("TLS metadata exceeds limits")
	}
	if i.State != "observed" && (i.ServerName != "" || i.ECHOffered || len(i.ALPN) > 0 || len(i.OfferedVersions) > 0) {
		return fmt.Errorf("TLS metadata without observed hello")
	}
	for _, v := range i.ALPN {
		if len(v) > 255 {
			return fmt.Errorf("invalid ALPN length")
		}
	}
	for _, v := range i.OfferedVersions {
		if len(v) > 16 {
			return fmt.Errorf("invalid TLS version length")
		}
	}
	return nil
}
func (i *TLSInfo) Label() string {
	if i == nil {
		return "—"
	}
	if i.ECHOffered {
		return "<ECH>"
	}
	if i.ServerName != "" {
		return i.ServerName
	}
	if i.State == "observed" {
		return "без SNI"
	}
	return "—"
}
