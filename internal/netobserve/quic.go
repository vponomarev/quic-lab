package netobserve

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"time"
)

const maxQUICPackets = 64
const quicWindow = 10 * time.Second

// QUICProbe passively inspects upstream datagrams using public Initial keys.
// Callers serialize access. Initial authentication is not peer authentication.
// Only a bounded ClientHello is retained; no application keys or payload IO.
type QUICProbe struct {
	info          TLSInfo
	version       string
	done          bool
	started       time.Time
	packets       int
	data, present []byte
	contiguous    int
	dcid          []byte
	wireVersion   uint32
	largest       uint64
	havePN        bool
}

func NewQUICProbe(missedStart bool) *QUICProbe {
	p := &QUICProbe{info: TLSInfo{State: "incomplete"}, started: time.Now()}
	if missedStart {
		p.finish("not_observed")
	}
	return p
}
func (p *QUICProbe) finish(s string) {
	p.info.State = s
	p.done = true
	p.data = nil
	p.present = nil
	p.dcid = nil
}
func (p *QUICProbe) expire() {
	if !p.done && time.Since(p.started) > quicWindow {
		p.finish("limit")
	}
}
func (p *QUICProbe) Info() *TLSInfo {
	if p == nil {
		return nil
	}
	p.expire()
	v := p.info
	v.ALPN = append([]string(nil), v.ALPN...)
	v.OfferedVersions = append([]string(nil), v.OfferedVersions...)
	return &v
}
func (p *QUICProbe) Version() string {
	if p == nil {
		return ""
	}
	p.expire()
	return p.version
}

type quicCursor struct {
	b   []byte
	bad bool
}

func (c *quicCursor) take(n int) []byte {
	if n < 0 || n > len(c.b) {
		c.bad = true
		c.b = nil
		return nil
	}
	v := c.b[:n]
	c.b = c.b[n:]
	return v
}
func (c *quicCursor) uint() uint64 {
	if len(c.b) == 0 {
		c.bad = true
		return 0
	}
	n := 1 << (c.b[0] >> 6)
	b := c.take(n)
	if c.bad {
		return 0
	}
	v := uint64(b[0] & 63)
	for _, x := range b[1:] {
		v = v<<8 | uint64(x)
	}
	return v
}
func (c *quicCursor) cid() []byte {
	b := c.take(1)
	if c.bad {
		return nil
	}
	if b[0] > 20 {
		c.bad = true
		return nil
	}
	return c.take(int(b[0]))
}

// All requested expansions fit one SHA-256 block.
func quicExpand(secret []byte, label string, n int) []byte {
	label = "tls13 " + label
	info := []byte{byte(n >> 8), byte(n), byte(len(label))}
	info = append(info, label...)
	info = append(info, 0, 1)
	h := hmac.New(sha256.New, secret)
	h.Write(info)
	return h.Sum(nil)[:n]
}
func quicKeys(dcid []byte, version uint32) (key, iv, hp []byte) {
	saltHex, prefix := "38762cf7f55934b34d179ae6a4c80cadccbb7f0a", "quic"
	if version == 0x6b3343cf {
		saltHex = "0dede3def700a6db819381be6e269dcbf9bd2ed9"
		prefix = "quicv2"
	}
	salt, _ := hex.DecodeString(saltHex)
	h := hmac.New(sha256.New, salt)
	h.Write(dcid)
	secret := quicExpand(h.Sum(nil), "client in", 32)
	return quicExpand(secret, prefix+" key", 16), quicExpand(secret, prefix+" iv", 12), quicExpand(secret, prefix+" hp", 16)
}
func quicPacketNumber(truncated uint64, n int, expected uint64) uint64 {
	window := uint64(1) << (8 * n)
	half := window / 2
	candidate := (expected & ^(window - 1)) | truncated
	if candidate+half <= expected && candidate < (uint64(1)<<62)-window {
		candidate += window
	} else if candidate > expected+half && candidate >= window {
		candidate -= window
	}
	return candidate
}
func (p *QUICProbe) Feed(b []byte) {
	if p == nil {
		return
	}
	p.expire()
	if p.done {
		return
	}
	if len(b) > 65535 {
		p.finish("limit")
		return
	}
	for len(b) > 0 && !p.done {
		p.packets++
		if p.packets > maxQUICPackets {
			p.finish("limit")
			return
		}
		if len(b) < 7 {
			p.finish("malformed")
			return
		}
		if b[0]&0xc0 != 0xc0 {
			p.finish("not_observed")
			return
		}
		version := binary.BigEndian.Uint32(b[1:5])
		if version != 1 && version != 0x6b3343cf {
			p.finish("not_observed")
			return
		}
		c := quicCursor{b: b[5:]}
		dcid := c.cid()
		c.cid()
		if c.bad {
			p.finish("malformed")
			return
		}
		typ := (b[0] >> 4) & 3
		initial := typ == 0
		retry := typ == 3
		if version == 0x6b3343cf {
			initial = typ == 1
			retry = typ == 0
		}
		if retry {
			p.finish("not_observed")
			return
		}
		if initial {
			token := c.uint()
			if token > uint64(len(c.b)) {
				p.finish("malformed")
				return
			}
			c.take(int(token))
		}
		size := c.uint()
		offset := len(b) - len(c.b)
		if c.bad || size > uint64(len(c.b)) {
			p.finish("malformed")
			return
		}
		end := offset + int(size)
		packet := b[:end]
		b = b[end:]
		if !initial {
			continue
		}
		if size < 20 {
			p.finish("malformed")
			return
		}
		if p.havePN && (p.wireVersion != version || !bytes.Equal(p.dcid, dcid)) {
			p.finish("not_observed")
			return
		}
		key, iv, hp := quicKeys(dcid, version)
		block, _ := aes.NewCipher(hp)
		var mask [16]byte
		block.Encrypt(mask[:], packet[offset+4:offset+20])
		first := packet[0] ^ (mask[0] & 15)
		pnLen := int(first&3) + 1
		header := append([]byte(nil), packet[:offset+pnLen]...)
		header[0] = first
		var truncated uint64
		for i := 0; i < pnLen; i++ {
			header[offset+i] ^= mask[i+1]
			truncated = truncated<<8 | uint64(header[offset+i])
		}
		expected := uint64(0)
		if p.havePN {
			expected = p.largest + 1
		}
		pn := quicPacketNumber(truncated, pnLen, expected)
		for i := 0; i < 8; i++ {
			iv[len(iv)-1-i] ^= byte(pn >> (8 * i))
		}
		block, _ = aes.NewCipher(key)
		aead, _ := cipher.NewGCM(block)
		plain, err := aead.Open(nil, iv, packet[offset+pnLen:], header)
		if err != nil {
			continue
		} // A corrupted datagram must not poison a later retransmission.
		if first&12 != 0 {
			p.finish("malformed")
			return
		}
		if !p.havePN {
			p.dcid = append([]byte(nil), dcid...)
			p.wireVersion = version
		}
		if !p.havePN || pn > p.largest {
			p.largest = pn
		}
		p.havePN = true
		p.version = "QUIC v1"
		if version == 0x6b3343cf {
			p.version = "QUIC v2"
		}
		p.frames(plain)
	}
}
func (p *QUICProbe) frames(b []byte) {
	c := quicCursor{b: b}
	for len(c.b) > 0 && !p.done && !c.bad {
		typ := c.uint()
		switch typ {
		case 0, 1: // PADDING / PING
		case 2, 3: // ACK / ACK_ECN: bounded by remaining plaintext.
			c.uint()
			c.uint()
			count := c.uint()
			c.uint()
			if count > uint64(len(c.b)/2) {
				c.bad = true
				break
			}
			for i := uint64(0); i < count; i++ {
				c.uint()
				c.uint()
			}
			if typ == 3 {
				c.uint()
				c.uint()
				c.uint()
			}
		case 6:
			offset, n := c.uint(), c.uint()
			if c.bad {
				break
			}
			if offset > MaxTLSHelloBytes || n > MaxTLSHelloBytes-offset {
				p.finish("limit")
				return
			}
			if n > uint64(len(c.b)) {
				c.bad = true
				break
			}
			fragment := c.take(int(n))
			if p.data == nil {
				p.data = make([]byte, MaxTLSHelloBytes)
				p.present = make([]byte, MaxTLSHelloBytes)
			}
			for i, v := range fragment {
				j := int(offset) + i
				if p.present[j] != 0 && p.data[j] != v {
					p.finish("malformed")
					return
				}
				p.data[j] = v
				p.present[j] = 1
			}
			for p.contiguous < len(p.data) && p.present[p.contiguous] != 0 {
				p.contiguous++
			}
		case 0x1c: // CONNECTION_CLOSE ends observation.
			c.uint()
			c.uint()
			n := c.uint()
			if n > uint64(len(c.b)) {
				c.bad = true
				break
			}
			c.take(int(n))
			if !c.bad {
				p.finish("not_observed")
			}
		default:
			c.bad = true // Other frames are forbidden at the Initial encryption level.
		}
	}
	if c.bad {
		p.finish("malformed")
		return
	}
	if p.done || p.contiguous < 4 {
		return
	}
	if p.data[0] != 1 {
		p.finish("not_observed")
		return
	}
	n := int(p.data[1])<<16 | int(p.data[2])<<8 | int(p.data[3])
	if n+4 > MaxTLSHelloBytes {
		p.finish("limit")
		return
	}
	if p.contiguous < n+4 {
		return
	}
	info, ok := parseClientHello(p.data[4 : n+4])
	if !ok {
		p.finish("malformed")
		return
	}
	p.info = info
	p.finish("observed")
}
