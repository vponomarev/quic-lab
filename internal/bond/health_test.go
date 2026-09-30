package bond

import (
	"context"
	"testing"
	"time"
)

func TestSizedProbeAndPayloadProgress(t *testing.T) {
	a, b := New(context.Background(), func(Record) bool { return true }), New(context.Background(), func(Record) bool { return true })
	defer a.Close()
	defer b.Close()
	pa, pb, _ := pair(time.Millisecond)
	if err := a.AddPath("wifi", pa); err != nil {
		t.Fatal(err)
	}
	if err := b.AddPath("wifi", pb); err != nil {
		t.Fatal(err)
	}
	if err := a.ProbeData("wifi"); err != nil {
		t.Fatal(err)
	}
	if err := a.Send(context.Background(), Record{Kind: Data, Flow: 1, Payload: make([]byte, Chunk)}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		p := a.Stats().Paths[0]
		if p.AckedBytes == Chunk && p.DataProbes == 1 && p.PendingBytes == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("missing sized probe or acknowledged payload", a.Stats())
}

// sizedDropPath simulates a link where tiny liveness replies pass but payload-sized records disappear.
type sizedDropPath struct{ Path }

func (p sizedDropPath) SendDatagram(b []byte) error {
	if len(b) > header+64 {
		return nil
	}
	return p.Path.SendDatagram(b)
}
func TestSmallPacketsDoNotCompleteSizedProbe(t *testing.T) {
	a, b := New(context.Background(), func(Record) bool { return true }), New(context.Background(), func(Record) bool { return true })
	defer a.Close()
	defer b.Close()
	pa, pb, _ := pair(time.Millisecond)
	a.AddPath("wifi", sizedDropPath{pa})
	b.AddPath("wifi", pb)
	if err := a.ProbeData("wifi"); err != nil {
		t.Fatal(err)
	}
	// Tiny ordinary ping replies arrive, independently of the blocked sized probe.
	a.mu.Lock()
	stamp := time.Now().UnixNano()
	a.sendOn("wifi", a.paths["wifi"], frame(ping, 0, 0, 0, stamp, false, nil), true)
	a.mu.Unlock()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		p := a.Stats().Paths[0]
		if p.Received > 0 {
			if p.DataProbes != 0 || !p.DataProbePending {
				t.Fatal("tiny reply credited as data probe", p)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("tiny liveness reply did not pass")
}
