package netobserve

import (
	"golang.org/x/net/dns/dnsmessage"
	"testing"
)

func TestDNSFacts(t *testing.T) {
	n := dnsmessage.MustNewName("example.test.")
	m := dnsmessage.Message{Header: dnsmessage.Header{ID: 42, Response: true, Truncated: true, RCode: dnsmessage.RCodeNameError}, Questions: []dnsmessage.Question{{Name: n, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}}, Answers: []dnsmessage.Resource{
		{Header: dnsmessage.ResourceHeader{Name: n, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET, TTL: 120}, Body: &dnsmessage.AResource{A: [4]byte{1, 2, 3, 4}}},
		{Header: dnsmessage.ResourceHeader{Name: n, Type: dnsmessage.TypeAAAA, Class: dnsmessage.ClassINET, TTL: 60}, Body: &dnsmessage.AAAAResource{AAAA: [16]byte{15: 1}}},
		{Header: dnsmessage.ResourceHeader{Name: n, Type: dnsmessage.TypeCNAME, Class: dnsmessage.ClassINET, TTL: 30}, Body: &dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName("alias.test.")}}}}
	b, e := m.Pack()
	if e != nil {
		t.Fatal(e)
	}
	got, e := DecodeDNS(b)
	if e != nil {
		t.Fatal(e)
	}
	if got.ID != 42 || !got.Truncated || got.RCode != 3 || !got.Response || len(got.Answers) != 3 || got.Answers[0].TTL != 120 || got.Answers[0].Class != 1 || got.Answers[0].Address != "1.2.3.4" || got.Answers[1].Address != "::1" || got.Answers[2].CNAME != "alias.test." {
		t.Fatalf("%+v", got)
	}
	f := got.Fields()
	if f["domain"] != "example.test" || f["transaction_id"] != uint16(42) || f["qtype"] != uint16(1) {
		t.Fatal(f)
	}
}
func TestDNSRejectMalformed(t *testing.T) {
	loop := []byte{0, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 192, 12, 0, 1, 0, 1}
	count := make([]byte, 12)
	count[4] = 255
	for _, b := range [][]byte{nil, make([]byte, 11), make([]byte, 4097), loop, count} {
		if _, e := DecodeDNS(b); e == nil {
			t.Fatalf("accepted %x", b)
		}
	}
}
