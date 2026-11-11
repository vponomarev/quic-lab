//go:build linux && !android && guiacceptance

package desktopui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/widget"
	"quiclab/internal/desktopcontrol"
)

// RunLiveAcceptance drives the real window and privileged controller. It is
// deliberately excluded from ordinary builds: running it changes live VPN
// connections and boot settings. Call from main as the configured desktop user.
func RunLiveAcceptance() error {
	if os.Geteuid() == 0 {
		return errors.New("live GUI acceptance requires the ordinary desktop user")
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return errors.New("live GUI acceptance requires a graphical session")
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = fmt.Sprintf("/run/user/%d", os.Geteuid())
	}
	lock, err := instanceLock(dir)
	if err != nil {
		return err
	}
	defer lock.Close()
	application := app.NewWithID("ru.vpnc.QuicLab.Acceptance")
	window := application.NewWindow("QUIC Lab — проверка")
	window.Resize(fyne.NewSize(620, 640))
	window.CenterOnScreen()
	h := &liveAcceptance{window: window, closed: make(chan struct{})}
	h.view = NewView(window, func(ctx context.Context, command desktopcontrol.Command) (desktopcontrol.Status, error) {
		status, err := desktopcontrol.Call(ctx, command)
		if command.Op == "status" {
			h.polls.Add(1)
		}
		return status, err
	})
	var once sync.Once
	window.SetOnClosed(func() { h.view.Close(); once.Do(func() { close(h.closed) }) })
	h.view.Start()
	result := make(chan error, 1)
	go func() {
		invocation, runErr := h.exercise()
		closeErr := h.closeWindow()
		if runErr == nil {
			runErr = closeErr
		}
		if runErr == nil {
			runErr = verifyClosedVPN(invocation)
		}
		if runErr != nil {
			if restoreErr := restoreLiveDefaults(); restoreErr != nil {
				runErr = fmt.Errorf("%v; restore QUIC/autostart failed: %v", runErr, restoreErr)
			}
		}
		result <- runErr
	}()
	window.ShowAndRun()
	return <-result
}

type liveAcceptance struct {
	window fyne.Window
	view   *View
	closed chan struct{}
	polls  atomic.Uint64
}

// ui bounds dispatch even if the user closes the window or the event loop
// stops. An expired queued callback must not perform a delayed UI action.
func (h *liveAcceptance) ui(action func() error) error {
	select {
	case <-h.closed:
		return errors.New("window closed during acceptance")
	default:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		var err error
		fyne.DoAndWait(func() {
			if ctx.Err() == nil {
				err = action()
			} else {
				err = ctx.Err()
			}
		})
		done <- err
	}()
	select {
	case err := <-done:
		return err
	case <-h.closed:
		return errors.New("window closed during acceptance")
	case <-ctx.Done():
		return errors.New("Fyne event dispatch timed out")
	}
}
func (h *liveAcceptance) wait(label string, timeout time.Duration, check func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ready := false
		err := h.ui(func() error { var err error; ready, err = check(); return err })
		if err != nil {
			return fmt.Errorf("%s: %w", label, err)
		}
		if ready {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("%s: timed out", label)
}
func (h *liveAcceptance) healthy(profile string) (desktopcontrol.Status, error) {
	var snapshot desktopcontrol.Status
	err := h.wait("fresh RTT for "+profile, 180*time.Second, func() (bool, error) {
		v := h.view
		if v.state.errText != "" {
			return false, errors.New(v.state.errText)
		}
		s := v.state.status
		if s == nil || v.state.busy || s.Active != "active" || s.Selected != profile || latency(*s, desktopcontrol.Monotonic()) == "RTT: —" {
			return false, nil
		}
		if v.title.Text != "VPN подключён" || v.rtt.Text == "RTT: —" {
			return false, nil
		}
		snapshot = *s
		return true, nil
	})
	if err == nil {
		fmt.Printf("GUI_ACCEPT profile=%s rtt_ms=%.1f fresh=true\n", profile, *snapshot.RTTMS)
	}
	return snapshot, err
}
func (h *liveAcceptance) tap(button *widget.Button) error {
	return h.ui(func() error {
		if button.Disabled() {
			return errors.New("required button is disabled")
		}
		button.Tapped(&fyne.PointEvent{})
		return nil
	})
}
func (h *liveAcceptance) exercise() (string, error) {
	initial, err := h.healthy("quic")
	if err != nil {
		return "", err
	}
	if !initial.Autostart {
		return "", errors.New("initial QUIC autostart is not enabled")
	}
	baseline := h.polls.Load()
	err = h.wait("three stable status polls", 30*time.Second, func() (bool, error) {
		v := h.view
		if v.toggle.Disabled() || v.reconnect.Disabled() || v.profiles.Disabled() || v.autostart.Disabled() {
			return false, errors.New("background polling disabled a control")
		}
		if v.state.status == nil || latency(*v.state.status, desktopcontrol.Monotonic()) == "RTT: —" || v.rtt.Text == "RTT: —" {
			return false, errors.New("RTT lost freshness during polling")
		}
		return h.polls.Load() >= baseline+3 && !v.state.inflight, nil
	})
	if err != nil {
		return "", err
	}
	fmt.Println("GUI_ACCEPT stable_polls=3 controls_enabled=true rtt_fresh=true")
	if err = h.tap(h.view.toggle); err != nil {
		return "", err
	}
	err = h.wait("disconnect button", 60*time.Second, func() (bool, error) {
		v := h.view
		if v.state.errText != "" {
			return false, errors.New(v.state.errText)
		}
		return v.state.status != nil && !v.state.busy && v.state.status.Active == "inactive" && v.rtt.Text == "RTT: —" && v.toggle.Text == "Подключить", nil
	})
	if err != nil {
		return "", err
	}
	if err = h.tap(h.view.toggle); err != nil {
		return "", err
	}
	connected, err := h.healthy("quic")
	if err != nil {
		return "", err
	}
	if err = h.tap(h.view.reconnect); err != nil {
		return "", err
	}
	reconnected, err := h.healthy("quic")
	if err != nil {
		return "", err
	}
	if connected.Invocation == "" || connected.Invocation == reconnected.Invocation {
		return "", errors.New("reconnect did not create a new invocation")
	}
	fmt.Println("GUI_ACCEPT disconnect_connect_reconnect=true")
	for _, profile := range []struct{ id, label string }{{"https", "HTTPS"}, {"awg", "AmneziaWG"}, {"vless", "VLESS"}, {"quic", "QUIC"}} {
		err = h.ui(func() error {
			v := h.view
			if v.profiles.Disabled() || v.toggle.Disabled() {
				return errors.New("profile controls are disabled")
			}
			v.profiles.SetSelected(profile.label)
			if v.state.choice != profile.id {
				return errors.New("requested profile is absent")
			}
			v.toggle.Tapped(&fyne.PointEvent{})
			if !v.state.confirming {
				return errors.New("profile switch did not show confirmation")
			}
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("select %s: %w", profile.id, err)
		}
		err = h.wait("native switch confirmation", 5*time.Second, func() (bool, error) {
			for _, overlay := range h.window.Canvas().Overlays().List() {
				if button := liveFindButton(overlay, "Переключить"); button != nil {
					button.Tapped(&fyne.PointEvent{})
					if h.view.state.confirming {
						return false, errors.New("confirmation did not dismiss")
					}
					return true, nil
				}
			}
			return false, nil
		})
		if err != nil {
			return "", err
		}
		if _, err = h.healthy(profile.id); err != nil {
			return "", err
		}
	}
	for _, enabled := range []bool{false, true} {
		err = h.ui(func() error {
			if h.view.autostart.Disabled() {
				return errors.New("autostart control is disabled")
			}
			if h.view.autostart.Checked != enabled {
				h.view.autostart.Tapped(&fyne.PointEvent{})
			}
			return nil
		})
		if err != nil {
			return "", err
		}
		err = h.wait("autostart checkbox", 30*time.Second, func() (bool, error) {
			v := h.view
			if v.state.errText != "" {
				return false, errors.New(v.state.errText)
			}
			return v.state.status != nil && !v.state.busy && v.state.status.Autostart == enabled && v.autostart.Checked == enabled, nil
		})
		if err != nil {
			return "", err
		}
	}
	fmt.Println("GUI_ACCEPT autostart_off_on=true native_confirmations=4")
	final, err := h.healthy("quic")
	if err != nil {
		return "", err
	}
	if final.Invocation == "" || !final.Autostart {
		return "", errors.New("final QUIC invocation or boot setting is missing")
	}
	if path := os.Getenv("GUI_SCREENSHOT"); path != "" {
		var captured image.Image
		if err = h.ui(func() error { captured = h.window.Canvas().Capture(); return nil }); err != nil {
			return "", err
		}
		file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
		if openErr != nil {
			return "", errors.New("cannot create GUI screenshot")
		}
		encodeErr := png.Encode(file, captured)
		closeErr := file.Close()
		if encodeErr != nil || closeErr != nil {
			return "", errors.New("cannot save GUI screenshot")
		}
		fmt.Println("GUI_ACCEPT screenshot_saved=true")
	}
	return final.Invocation, nil
}
func liveFindButton(object fyne.CanvasObject, text string) *widget.Button {
	if object == nil || !object.Visible() {
		return nil
	}
	switch object := object.(type) {
	case *widget.Button:
		if object.Text == text && !object.Disabled() {
			return object
		}
	case *widget.PopUp:
		return liveFindButton(object.Content, text)
	case *fyne.Container:
		for _, child := range object.Objects {
			if button := liveFindButton(child, text); button != nil {
				return button
			}
		}
	default:
		// Fyne 2.8 wraps each PopUp in an internal OverlayContainer. Its
		// Content field is exported, but its Go type cannot be imported.
		// Inspect that public field without creating a second renderer or
		// touching any unexported widget state.
		value := reflect.ValueOf(object)
		if value.Kind() == reflect.Pointer && !value.IsNil() {
			value = value.Elem()
		}
		if value.Kind() == reflect.Struct {
			content := value.FieldByName("Content")
			if content.IsValid() && content.CanInterface() {
				if child, ok := content.Interface().(fyne.CanvasObject); ok {
					return liveFindButton(child, text)
				}
			}
		}
	}
	return nil
}
func (h *liveAcceptance) closeWindow() error {
	select {
	case <-h.closed:
		return nil
	default:
	}
	done := make(chan struct{})
	go func() { fyne.DoAndWait(func() { h.window.Close() }); close(done) }()
	select {
	case <-h.closed:
		return nil
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		return errors.New("window close timed out")
	}
}
func verifyClosedVPN(invocation string) error {
	// Allow the window's cancellation to reach its in-flight status request.
	time.Sleep(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s, err := desktopcontrol.Call(ctx, desktopcontrol.Command{Op: "status"})
	if err != nil {
		return errors.New(desktopcontrol.ErrorText(err))
	}
	if s.Active != "active" || s.Selected != "quic" || !s.Autostart || s.Invocation != invocation {
		return errors.New("closing the GUI changed the VPN invocation or final state")
	}
	fmt.Println("GUI_ACCEPT close_preserves_vpn=true profile=quic autostart=true")
	return nil
}
func restoreLiveDefaults() error {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	var lastErr error
	for ctx.Err() == nil {
		s, err := desktopcontrol.Call(ctx, desktopcontrol.Command{Op: "status"})
		if err == nil && (s.Selected != "quic" || s.Active != "active") {
			s, err = desktopcontrol.Call(ctx, desktopcontrol.Command{Op: "connect", Profile: "quic"})
		}
		if err == nil && !s.Autostart {
			enabled := true
			s, err = desktopcontrol.Call(ctx, desktopcontrol.Command{Op: "autostart", Enabled: &enabled})
		}
		if err == nil && s.Selected == "quic" && s.Active == "active" && s.Autostart {
			fmt.Println("GUI_ACCEPT recovery_profile=quic autostart=true")
			return nil
		}
		lastErr = err
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
	if lastErr != nil {
		return errors.New(desktopcontrol.ErrorText(lastErr))
	}
	return errors.New("recovery deadline exceeded")
}
