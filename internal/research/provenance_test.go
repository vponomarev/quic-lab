package research

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestProvenanceValidationAndIdentity(t *testing.T) {
	r := Record{SchemaVersion: 1, Type: "gap", ID: "a", At: time.Now(), Data: []byte("{}")}
	legacy := recordKey(r)
	if e := validate(r); e != nil {
		t.Fatal(e)
	}
	r.Origin = "client"
	r.ReceivedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if legacy != recordKey(r) {
		t.Fatal("legacy retry would duplicate")
	}
	var b bytes.Buffer
	if e := EncodeRecord(&b, r); e != nil {
		t.Fatal(e)
	}
	var decoded Record
	if e := json.Unmarshal(bytes.TrimSpace(b.Bytes()), &decoded); e != nil {
		t.Fatal(e)
	}
	if decoded.Origin != r.Origin || decoded.ReceivedAt != r.ReceivedAt {
		t.Fatal(decoded)
	}
	r.Origin = "server_awg"
	if legacy == recordKey(r) {
		t.Fatal("sources collide")
	}
	if e := validate(r); e != nil {
		t.Fatal(e)
	}
	r.Origin = "spoof"
	if validate(r) == nil {
		t.Fatal("unknown origin")
	}
	r.Origin = "client"
	r.ReceivedAt = "not a time"
	if validate(r) == nil {
		t.Fatal("invalid receipt")
	}
}
