//go:build windows

package windowsengine

import (
	"context"
	"testing"
	"time"
)

func TestWindowsWatchRegistrationCleanup(t *testing.T) {
	_, close, err := WatchWindowsIPv4(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { close(); closeChannel(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("notification cleanup blocked")
	}
}
func closeChannel(c chan struct{}) { close(c) }
