package mobile

import (
	"encoding/binary"
	"golang.org/x/net/dns/dnsmessage"
	"testing"
)

func TestResearchDNSAnswerAndBounds(t *testing.T) {
	name := dnsmessage.MustNewName("example.test.")
	message := dnsmessage.Message{Header: dnsmessage.Header{ID: 42, Response: true}, Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}, Answers: []dnsmessage.Resource{{Header: dnsmessage.ResourceHeader{Name: name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 120}, Body: &dnsmessage.AResource{A: [4]byte{1, 2, 3, 4}}}}}
	packet, err := message.Pack()
	if err != nil {
		t.Fatal(err)
	}
	data, ok := parseResearchDNS(packet)
	if !ok {
		t.Fatal("valid DNS rejected")
	}
	answers := data["answers"].([]map[string]any)
	if data["domain"] != "example.test" || data["question"] != "example.test." || data["transaction_id"] != uint16(42) || len(answers) != 1 || answers[0]["address"] != "1.2.3.4" || answers[0]["ttl"] != uint32(120) {
		t.Fatalf("wrong DNS metadata: %#v", data)
	}
	for _, bad := range [][]byte{packet[:5], make([]byte, 4097), append([]byte(nil), packet...)} {
		if len(bad) == len(packet) {
			binary.BigEndian.PutUint16(bad[6:8], 65535)
		}
		if _, ok := parseResearchDNS(bad); ok {
			t.Fatal("unbounded or truncated DNS accepted")
		}
	}
}
