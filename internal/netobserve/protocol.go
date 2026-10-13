package netobserve

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"time"
)

// ProtocolInfo is payload evidence, not a port-based guess or negotiated ALPN.
type ProtocolInfo struct {
	Name     string `json:"name"`
	Evidence string `json:"evidence"`
}

func (i *ProtocolInfo) Validate() error {
	if i == nil {
		return nil
	}
	expected := map[string]string{"TLS": "tls_client_hello", "QUIC v1": "quic_initial", "QUIC v2": "quic_initial", "HTTP": "http_request_line", "DNS": "dns_message"}
	if expected[i.Name] == "" || expected[i.Name] != i.Evidence {
		return errors.New("invalid detected protocol evidence")
	}
	return nil
}

// ProtocolProbe is owned by one flow and serialized by its existing consent lock.
// It retains at most a TLS/QUIC hello or a 4096-byte cleartext request prefix.
// The 10-second collection window is checked on Feed/Info; no timers or I/O.
type ProtocolProbe struct {
	tls                         *TLSProbe
	quic                        *QUICProbe
	finalQUIC                   *TLSInfo
	detected                    *ProtocolInfo
	protocol, port              int64
	prefix                      []byte
	started                     time.Time
	done, missed, quicCandidate bool
	packets                     int
}

func NewProtocolProbe(protocol, port int64, missedStart bool) *ProtocolProbe {
	p := &ProtocolProbe{protocol: protocol, port: port, missed: missedStart, started: time.Now()}
	if protocol == 6 {
		p.tls = NewTLSProbe(missedStart)
	} else if protocol == 17 {
		p.quic = NewQUICProbe(missedStart)
	}
	return p
}
func (p *ProtocolProbe) expire() {
	if p == nil || p.done || time.Since(p.started) <= 10*time.Second {
		return
	}
	p.prefix = nil
	p.done = true
	// Finalize incomplete metadata and release raw buffers on the observation deadline.
	if p.tls != nil && !p.tls.done {
		p.tls.finish("not_observed")
	}
	if p.quic != nil {
		p.finalQUIC = p.quic.Info()
		if p.finalQUIC.State == "incomplete" {
			p.finalQUIC.State = "not_observed"
		}
		p.quic = nil
	}
}
func (p *ProtocolProbe) Detected() *ProtocolInfo {
	if p == nil {
		return nil
	}
	p.expire()
	if p.detected == nil {
		return nil
	}
	v := *p.detected
	return &v
}
func (p *ProtocolProbe) Info() *TLSInfo {
	if p == nil {
		return nil
	}
	p.expire()
	if p.tls != nil {
		return p.tls.Info()
	}
	if p.quic != nil && p.quicCandidate {
		return p.quic.Info()
	}
	if p.quicCandidate && p.finalQUIC != nil {
		v := *p.finalQUIC
		return &v
	}
	return nil
}
func (p *ProtocolProbe) classify(name, evidence string) {
	p.detected = &ProtocolInfo{name, evidence}
	p.prefix = nil
	p.done = true
}
func (p *ProtocolProbe) Feed(b []byte) {
	if p == nil {
		return
	}
	p.expire()
	if p.done || p.missed || len(b) == 0 {
		return
	}
	if p.protocol == 17 {
		p.packets++
		if p.packets > 64 {
			p.finalQUIC = p.quic.Info()
			p.finalQUIC.State = "limit"
			p.quic = nil
			p.done = true
			return
		}
		if p.port == 53 && len(b) <= 4096 {
			if m, e := DecodeDNS(b); e == nil && !m.Response && len(m.Questions) > 0 {
				p.quic = nil
				p.classify("DNS", "dns_message")
				return
			}
		}
		if len(b) >= 5 && b[0]&0xc0 == 0xc0 {
			p.quicCandidate = true
		}
		p.quic.Feed(b)
		if v := p.quic.Version(); v != "" {
			p.detected = &ProtocolInfo{v, "quic_initial"}
		}
		if p.quic.Info().State == "observed" {
			p.done = true
		}
		return
	}
	if p.protocol != 6 {
		p.done = true
		return
	}
	if len(p.prefix) < 4096 {
		n := min(len(b), 4096-len(p.prefix))
		p.prefix = append(p.prefix, b[:n]...)
	}
	p.tls.Feed(b)
	if p.tls.Info().State == "observed" {
		p.classify("TLS", "tls_client_hello")
		return
	}
	if p.tls.Info().State != "not_tls" {
		return
	}
	// An opaque non-TLS prefix can only be HTTP or length-framed DNS here.
	if p.port == 53 && len(p.prefix) >= 2 {
		size := int(binary.BigEndian.Uint16(p.prefix[:2]))
		if size > 4094 || size < 12 {
			p.done = true
			p.prefix = nil
			return
		}
		if len(p.prefix) >= size+2 {
			if m, e := DecodeDNS(p.prefix[2 : 2+size]); e == nil && !m.Response && len(m.Questions) > 0 {
				p.tls = nil
				p.classify("DNS", "dns_message")
				return
			}
			p.done = true
			p.prefix = nil
			return
		}
	}
	if i := bytes.Index(p.prefix, []byte("\r\n")); i >= 0 {
		fields := strings.Split(string(p.prefix[:i]), " ")
		if len(fields) == 3 && fields[1] != "" && (fields[2] == "HTTP/1.0" || fields[2] == "HTTP/1.1") {
			switch fields[0] {
			case "GET", "HEAD", "POST", "PUT", "DELETE", "CONNECT", "OPTIONS", "TRACE", "PATCH":
				p.tls = nil
				p.classify("HTTP", "http_request_line")
				return
			}
		}
		p.done = true
		p.prefix = nil
	} else if len(p.prefix) == 4096 {
		p.done = true
		p.prefix = nil
	}
}
