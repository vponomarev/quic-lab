package gateway

import (
	"context"
	"crypto/tls"
	"quiclab/internal/research"
	"testing"
	"time"
)

func TestResearchOriginAuthoritative(t *testing.T) {
	for _, origin := range []string{"", "server_awg"} {
		var got research.Record
		receiver := NewResearchReceiver(func(tls.ConnectionState) (string, string, bool) { return "user", "device", true }, func(r research.Record) bool { got = r; return true })
		before := time.Now().UTC()
		r := research.Record{SchemaVersion: 1, Type: "gap", ID: "a", At: before.Add(-time.Hour), Generation: 1, Origin: origin, ReceivedAt: "untrusted", Data: []byte("{}")}
		reply := receiver.Receive(context.Background(), tls.ConnectionState{}, ResearchBatch{Session: "0123456789abcdef0123456789abcdef", Generation: 1, Enabled: true, Records: []research.Record{r}})
		received, e := time.Parse(time.RFC3339Nano, got.ReceivedAt)
		if reply.Accepted != 1 || got.Origin != "client" || e != nil || received.Before(before) || received.After(time.Now()) || !got.At.Equal(r.At) {
			t.Fatalf("%+v %+v %v", reply, got, e)
		}
	}
}
