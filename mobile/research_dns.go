package mobile

import (
	"net"
	"quiclab/internal/netobserve"
	"strconv"
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
	if !c.active || c.stopped || f.closed || !f.recording || (c.eligible != nil && !c.eligible(f)) {
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
	valid := c.active && !c.stopped && c.generation == job.generation && job.flow.recording && (c.eligible == nil || c.eligible(job.flow))
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
	if !c.active || c.stopped || c.generation != job.generation || !job.flow.recording || (c.eligible != nil && !c.eligible(job.flow)) {
		return
	}
	data["route"] = job.flow.exit.Route
	data["exit_id"] = job.flow.exit.ID
	data["exit_type"] = job.flow.exit.Type
	data["app"] = job.flow.app
	data["protocol"] = "udp"
	data["source"] = net.JoinHostPort(job.flow.local, strconv.FormatInt(job.flow.localPort, 10))
	data["destination"] = net.JoinHostPort(job.flow.remote, strconv.FormatInt(job.flow.remotePort, 10))
	c.enqueueLocked("dns", job.flow.id, data, job.at)
}
func parseResearchDNS(packet []byte) (map[string]any, bool) {
	message, err := netobserve.DecodeDNS(packet)
	if err != nil {
		return nil, false
	}
	return message.Fields(), true
}
