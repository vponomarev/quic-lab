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

func TestOriginFilterPreservesSourceBoundary(t *testing.T) {
	r := Record{Origin: "server_gateway", Type: "gap", At: time.Now(), Data: []byte("{}")}
	if matches(r, Query{Origin: "client"}) {
		t.Fatal("server gap crossed source filter")
	}
	if !matches(r, Query{Origin: "server_gateway"}) || !matches(r, Query{}) {
		t.Fatal("server source hidden")
	}
	r.Origin = ""
	if matches(r, Query{Origin: "client"}) {
		t.Fatal("legacy provenance invented")
	}
	if !matches(r, Query{Origin: "legacy_unknown"}) {
		t.Fatal("legacy missing")
	}
}
