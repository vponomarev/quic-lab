package mobile

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCaptureScopeAndStop(t *testing.T) {
	StopDebugCapture()
	defer StopDebugCapture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	up := &captureUpload{cfg: captureConfig{Kind: "echo", Host: "lab.example"}, ctx: ctx, cancel: cancel, queue: make(chan []byte, 1)}
	captureState.Lock()
	captureState.active = up
	captureState.Unlock()
	if debugKeyLog("vpn", "lab.example") != nil || debugKeyLog("echo", "other.example") != nil {
		t.Fatal("logged unrelated connection")
	}
	writer := debugKeyLog("echo", "lab.example")
	if writer == nil {
		t.Fatal("missing logger")
	}
	writer.Write([]byte("test"))
	writer.Write([]byte("full"))
	if up.dropped != 1 {
		t.Fatal("handshake must not block on full queue")
	}
	StopDebugCapture()
	if debugKeyLog("echo", "lab.example") != nil {
		t.Fatal("logger active after stop")
	}
}
func TestCaptureConfigValidation(t *testing.T) {
	c := captureConfig{Kind: "echo", Host: "lab.example", URL: "https://lab.example/lab/capture/keys?id=" + strings.Repeat("a", 64), Token: strings.Repeat("b", 64), Until: time.Now().Add(time.Minute).Unix()}
	b, _ := json.Marshal(c)
	if ValidateDebugCapture(string(b)) != nil {
		t.Fatal("valid QR")
	}
	c.URL = "http://lab.example/lab/capture/keys"
	b, _ = json.Marshal(c)
	if ValidateDebugCapture(string(b)) == nil {
		t.Fatal("plaintext upload")
	}
}
