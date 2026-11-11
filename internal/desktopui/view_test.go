//go:build linux

package desktopui

import (
	"context"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"quiclab/internal/desktopcontrol"
)

func TestWidgetsPreserveChoiceAndAutostartDuringPoll(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	window := app.NewWindow("QUIC Lab")
	defer window.Close()
	callbacks := make(chan func(), 8)
	calls := make(chan desktopcontrol.Command, 8)
	responses := make(chan desktopcontrol.Status, 8)
	request := func(ctx context.Context, c desktopcontrol.Command) (desktopcontrol.Status, error) {
		calls <- c
		select {
		case s := <-responses:
			return s, nil
		case <-ctx.Done():
			return desktopcontrol.Status{}, ctx.Err()
		}
	}
	now := 100.0
	v := newView(window, request, func(f func()) { callbacks <- f }, func() float64 { return now })
	defer v.Close()
	drain := func() {
		t.Helper()
		select {
		case f := <-callbacks:
			f()
		case <-time.After(3 * time.Second):
			t.Fatal("UI completion timeout")
		}
	}
	take := func(op string) {
		t.Helper()
		select {
		case c := <-calls:
			if c.Op != op {
				t.Fatalf("want %s, got %+v", op, c)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("request timeout")
		}
	}
	v.poll()
	take("status")
	responses <- sampleStatus()
	drain()
	if v.toggle.Text != "Отключить" || v.toggle.Disabled() || !v.autostart.Checked {
		t.Fatal("initial controls incorrect")
	}
	if !strings.Contains(v.traffic.Text, "32.0 КиБ") || strings.Contains(v.events.Text, "secret") {
		t.Fatal("traffic/events incorrect")
	}
	v.profiles.SetSelected("HTTPS")
	v.poll()
	take("status")
	if v.toggle.Disabled() || v.autostart.Disabled() || v.profiles.Disabled() || v.reconnect.Disabled() {
		t.Fatal("background poll disabled controls")
	}
	v.autostart.SetChecked(false)
	if !v.autostart.Disabled() {
		t.Fatal("pending action left controls enabled")
	}
	responses <- sampleStatus()
	drain()
	take("autostart")
	if v.autostart.Checked {
		t.Fatal("stale poll reverted checkbox")
	}
	next := sampleStatus()
	next.Autostart = false
	responses <- next
	drain()
	if v.autostart.Checked || v.autostart.Disabled() || v.profiles.Selected != "HTTPS" {
		t.Fatal("action or selection lost")
	}
	v.poll()
	take("status")
	now = 106
	v.poll()
	if v.rtt.Text != "RTT: —" || !strings.Contains(v.title.Text, "свежих") {
		t.Fatal("inflight RTT did not expire")
	}
	responses <- next
	drain()
	// The optional artifact exercises the same view as Run, without a controller.
	if path := os.Getenv("GUI_SCREENSHOT"); path != "" {
		window.Resize(fyne.NewSize(620, 640))
		window.Show()
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, window.Canvas().Capture())
		closeErr := file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	v.Close()
	v.poll()
	select {
	case c := <-calls:
		t.Fatalf("closing GUI sent command: %+v", c)
	default:
	}
}

func TestWidgetsCloseCancelsBlockedRequestWithoutDisconnect(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	window := app.NewWindow("QUIC Lab")
	defer window.Close()
	entered := make(chan desktopcontrol.Command, 1)
	stopped := make(chan struct{})
	v := newView(window, func(ctx context.Context, c desktopcontrol.Command) (desktopcontrol.Status, error) {
		entered <- c
		<-ctx.Done()
		close(stopped)
		return desktopcontrol.Status{}, ctx.Err()
	}, func(func()) { t.Error("completion scheduled after close") }, func() float64 { return 100 })
	v.poll()
	select {
	case c := <-entered:
		if c.Op != "status" {
			t.Fatal(c)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request never started")
	}
	v.Close()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("close did not cancel request")
	}
}

func TestWidgetProfileSwitchConfirmation(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	window := app.NewWindow("QUIC Lab")
	defer window.Close()
	calls := make(chan desktopcontrol.Command, 2)
	callbacks := make(chan func(), 2)
	v := newView(window, func(ctx context.Context, c desktopcontrol.Command) (desktopcontrol.Status, error) {
		calls <- c
		result := sampleStatus()
		result.Selected = c.Profile
		return result, nil
	}, func(f func()) { callbacks <- f }, func() float64 { return 100 })
	defer v.Close()
	v.state = *loadedState(t)
	v.render()
	v.profiles.SetSelected("HTTPS")
	var answer func(bool)
	v.confirm = func(done func(bool)) { answer = done }
	test.Tap(v.toggle)
	if answer == nil || !v.state.confirming || !v.toggle.Disabled() {
		t.Fatal("profile switch bypassed confirmation")
	}
	v.poll()
	select {
	case c := <-calls:
		t.Fatalf("request during confirmation: %+v", c)
	default:
	}
	answer(false)
	if v.state.confirming || v.toggle.Disabled() {
		t.Fatal("cancel left controls locked")
	}
	select {
	case c := <-calls:
		t.Fatalf("cancel sent command: %+v", c)
	default:
	}
	test.Tap(v.toggle)
	answer(true)
	select {
	case c := <-calls:
		if c.Op != "connect" || c.Profile != "https" {
			t.Fatal(c)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("confirmed switch did not dispatch")
	}
	select {
	case f := <-callbacks:
		f()
	case <-time.After(3 * time.Second):
		t.Fatal("switch response timeout")
	}
	if v.toggle.Text != "Отключить" || v.profiles.Selected != "HTTPS" {
		t.Fatal("switch response not reflected")
	}
}
