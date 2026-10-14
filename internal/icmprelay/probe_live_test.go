package icmprelay

import (
	"context"
	"encoding/binary"
	"os"
	"testing"
	"time"
)

func TestLiveProbeTTL(t *testing.T) {
	if os.Getenv("QUIC_LAB_ICMP_LIVE") != "1" {
		t.Skip("explicit opt-in raw socket network test")
	}
	for _, ttl := range []byte{1, 128} {
		p := packet()
		p[8] = ttl
		p[10] = 0
		p[11] = 0
		binary.BigEndian.PutUint16(p[10:12], Checksum(p[:20]))
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		r, e := Probe(ctx, "", p)
		cancel()
		if e != nil {
			t.Fatal(e)
		}
		want := byte(0)
		if ttl == 1 {
			want = 11
		}
		if !ValidateResponse(p, r) || r[20] != want {
			t.Fatalf("TTL %d got %x", ttl, r)
		}
		t.Logf("request TTL=%d response type=%d peer=%v TTL=%d", ttl, r[20], r[12:16], r[8])
	}
}
