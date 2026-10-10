package mobile

import (
	"encoding/binary"
	"golang.org/x/net/dns/dnsmessage"
	"net"
	"strconv"
	"strings"
	"time"
)

// Only ordinary UDP/53 datagrams are DNS evidence. No DoH/DoT or IP-name inference.
func (f *researchFlow) dnsPacket(packet []byte, upstream bool) {
	if f == nil || f.protocol != 17 || f.remotePort != 53 || len(packet) < 12 || len(packet) > 4096 {
		return
	}
	c := f.collector
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active || c.stopped || f.closed {
		return
	}
	job := researchDNSJob{flow: f, generation: c.generation, packet: append([]byte(nil), packet...), upstream: upstream, at: time.Now().UTC()}
	select {
	case c.dns <- job:
	default:
		c.dropped++
	}
}
func (c *researchCollector) recordDNS(job researchDNSJob) {
	c.mu.Lock()
	valid := c.active && !c.stopped && c.generation == job.generation
	c.mu.Unlock()
	if !valid {
		return
	}
	data, ok := parseResearchDNS(job.packet)
	if !ok {
		return
	}
	data["direction"] = "response"
	if job.upstream {
		data["direction"] = "query"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.active || c.stopped || c.generation != job.generation {
		return
	}
	data["app"] = job.flow.app
	data["protocol"] = "udp"
	data["source"] = net.JoinHostPort(job.flow.local, strconv.FormatInt(job.flow.localPort, 10))
	data["destination"] = net.JoinHostPort(job.flow.remote, strconv.FormatInt(job.flow.remotePort, 10))
	c.enqueueLocked("dns", job.flow.id, data, job.at)
}
func parseResearchDNS(packet []byte) (map[string]any, bool) {
	if len(packet) < 12 || len(packet) > 4096 {
		return nil, false
	}
	// Header counts are attacker-controlled; cap them before any parser allocation.
	if binary.BigEndian.Uint16(packet[4:6]) > 8 || binary.BigEndian.Uint16(packet[6:8]) > 32 || binary.BigEndian.Uint16(packet[8:10]) > 32 || binary.BigEndian.Uint16(packet[10:12]) > 32 {
		return nil, false
	}
	var message dnsmessage.Message
	if message.Unpack(packet) != nil {
		return nil, false
	}
	questions := make([]map[string]any, 0, len(message.Questions))
	for _, q := range message.Questions {
		questions = append(questions, map[string]any{"name": q.Name.String(), "qtype": uint16(q.Type)})
	}
	answers := make([]map[string]any, 0, len(message.Answers))
	for _, r := range message.Answers {
		row := map[string]any{"name": r.Header.Name.String(), "type": uint16(r.Header.Type), "ttl": r.Header.TTL}
		switch body := r.Body.(type) {
		case *dnsmessage.AResource:
			row["address"] = net.IP(body.A[:]).String()
		case *dnsmessage.AAAAResource:
			row["address"] = net.IP(body.AAAA[:]).String()
		case *dnsmessage.CNAMEResource:
			row["cname"] = body.CNAME.String()
		default:
			continue
		}
		answers = append(answers, row)
	}
	out := map[string]any{"transaction_id": message.Header.ID, "response": message.Header.Response, "rcode": uint16(message.Header.RCode), "truncated": message.Header.Truncated, "questions": questions, "answers": answers}
	if len(questions) > 0 {
		out["question"] = questions[0]["name"]
		out["domain"] = strings.TrimSuffix(questions[0]["name"].(string), ".")
		out["qtype"] = questions[0]["qtype"]
	}
	return out, true
}
