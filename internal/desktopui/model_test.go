//go:build linux

package desktopui

import (
	"errors"
	"math"
	"quiclab/internal/desktopcontrol"
	"strings"
	"testing"
)

func sampleStatus() desktopcontrol.Status {
	rtt, at := 12.5, 100.0
	return desktopcontrol.Status{Selected: "quic", Profiles: []desktopcontrol.Profile{{ID: "quic", Label: "QUIC"}, {ID: "https", Label: "HTTPS"}, {ID: "awg", Label: "AmneziaWG"}, {ID: "vless", Label: "VLESS"}}, Active: "active", Autostart: true, Invocation: "a", SessionState: "running", Readiness: "vpn_dns_ready", TransportState: "ready", RTTMS: &rtt, ProbeAt: &at, ProbeIntervalMS: 1000, Probe: "echo", TXBytes: 2048, RXBytes: 32768, Events: []string{"running", "vpn_dns_ready", "secret=do-not-render"}}
}

func TestFreshnessAndReadiness(t *testing.T) {
	s := sampleStatus()
	if got := latency(s, 104); !strings.Contains(got, "12.5") {
		t.Fatal(got)
	}
	if title, _ := describe(s, 106); title != "Нет свежих ответов VPN" {
		t.Fatal(title)
	}
	if got := latency(s, 106); got != "RTT: —" {
		t.Fatal(got)
	}
	for _, change := range []func(*desktopcontrol.Status){
		func(s *desktopcontrol.Status) { s.Active = "inactive" },
		func(s *desktopcontrol.Status) { s.SessionState = "reconnecting" },
		func(s *desktopcontrol.Status) { s.Readiness = "unavailable" },
		func(s *desktopcontrol.Status) { s.TransportState = "checking" },
		func(s *desktopcontrol.Status) { v := math.NaN(); s.RTTMS = &v },
		func(s *desktopcontrol.Status) { v := math.Inf(1); s.ProbeAt = &v },
		func(s *desktopcontrol.Status) { s.ProbeIntervalMS = 2000 },
		func(s *desktopcontrol.Status) { v := 105.0; s.ProbeAt = &v },
	} {
		bad := sampleStatus()
		change(&bad)
		if got := latency(bad, 104); got != "RTT: —" {
			t.Fatal(got)
		}
	}
}

func TestTrafficResetsAcrossInvocationsAndCounterReset(t *testing.T) {
	var rate trafficRate
	if up, down := rate.update("a", 100, 200, 1); up != 0 || down != 0 {
		t.Fatal(up, down)
	}
	if up, down := rate.update("a", 300, 600, 3); up != 100 || down != 200 {
		t.Fatal(up, down)
	}
	if up, down := rate.update("b", 5000, 9000, 4); up != 0 || down != 0 {
		t.Fatal(up, down)
	}
	if up, down := rate.update("b", 10, 20, 5); up != 0 || down != 0 {
		t.Fatal(up, down)
	}
	if got := humanBytes(1024); got != "1.0 КиБ" {
		t.Fatal(got)
	}
}

func loadedState(t *testing.T) *state {
	t.Helper()
	s := &state{}
	if !s.begin(desktopcontrol.Command{Op: "status"}) {
		t.Fatal("initial poll rejected")
	}
	s.finish(sampleStatus(), nil, 100)
	return s
}

func TestPollingKeepsControlsAndUserProfileChoice(t *testing.T) {
	s := loadedState(t)
	s.choice = "https"
	before := s.controls()
	if !s.begin(desktopcontrol.Command{Op: "status"}) {
		t.Fatal("poll rejected")
	}
	if got := s.controls(); got != before {
		t.Fatalf("poll changed controls: %+v -> %+v", before, got)
	}
	s.finish(sampleStatus(), nil, 101)
	if s.choice != "https" {
		t.Fatal("poll reverted choice")
	}
	if strings.Contains(s.events, "secret") {
		t.Fatal("raw event escaped allowlist")
	}
}

func TestOneActionDuringPollSurvivesStaleOrFailedResponse(t *testing.T) {
	for _, pollErr := range []error{nil, errors.New("offline")} {
		s := loadedState(t)
		s.begin(desktopcontrol.Command{Op: "status"})
		enabled := false
		if s.begin(desktopcontrol.Command{Op: "autostart", Enabled: &enabled}) {
			t.Fatal("action dispatched concurrently")
		}
		if !s.busy || s.pending == nil {
			t.Fatal("action not queued")
		}
		if s.begin(desktopcontrol.Command{Op: "disconnect"}) {
			t.Fatal("second action accepted")
		}
		next := s.finish(sampleStatus(), pollErr, 101)
		if next == nil || next.Op != "autostart" || *next.Enabled {
			t.Fatal("pending action lost", next)
		}
		if !s.busy || !s.inflight {
			t.Fatal("queued action not marked in flight")
		}
		result := sampleStatus()
		result.Autostart = false
		s.finish(result, nil, 102)
		if s.busy || s.inflight || s.status.Autostart {
			t.Fatal("action response not applied")
		}
	}
}

func TestPendingCommandsCannotKeepOldRTT(t *testing.T) {
	for _, op := range []string{"status", "autostart", "reconnect"} {
		s := loadedState(t)
		s.begin(desktopcontrol.Command{Op: op})
		_, _, rtt := s.health(106)
		if rtt != "RTT: —" {
			t.Fatalf("%s retained RTT: %s", op, rtt)
		}
		if op == "reconnect" {
			_, _, rtt = s.health(101)
			if rtt != "RTT: —" {
				t.Fatal("reconnect retained RTT")
			}
		}
	}
}

func TestUnknownProfileDisconnectAndOfflineControls(t *testing.T) {
	s := loadedState(t)
	s.status.Selected = ""
	if c := s.controls(); c.Toggle != "Отключить" || !c.CanToggle || c.CanReconnect {
		t.Fatal(c)
	}
	cmd, confirm := s.connectionCommand()
	if cmd.Op != "disconnect" || confirm {
		t.Fatal(cmd, confirm)
	}
	s.begin(desktopcontrol.Command{Op: "status"})
	s.finish(desktopcontrol.Status{}, errors.New("secret endpoint"), 101)
	if s.controls().CanToggle || s.status != nil {
		t.Fatal("offline controls enabled")
	}
	if strings.Contains(s.errText, "secret") {
		t.Fatal("raw error escaped")
	}
}

func TestProfileSwitchRequiresConfirmationAndCloseDropsPending(t *testing.T) {
	s := loadedState(t)
	s.choice = "https"
	cmd, confirm := s.connectionCommand()
	if cmd.Op != "connect" || cmd.Profile != "https" || !confirm {
		t.Fatal(cmd, confirm)
	}
	s.confirming = true
	if s.begin(desktopcontrol.Command{Op: "status"}) {
		t.Fatal("poll ran during confirmation")
	}
	s.confirming = false
	s.begin(desktopcontrol.Command{Op: "status"})
	s.begin(cmd)
	s.close()
	if next := s.finish(sampleStatus(), nil, 101); next != nil {
		t.Fatal("close dispatched pending action")
	}
	if s.begin(desktopcontrol.Command{Op: "disconnect"}) {
		t.Fatal("closed view accepted command")
	}
}

func TestActionErrorSurvivesHealthyPollUntilNextAction(t *testing.T) {
	// This real controller error is returned before filesystem or socket access.
	// It has the same safe-message behavior as start, rollback, and boot errors.
	_, commandErr := desktopcontrol.New("relative-test-path")
	if commandErr == nil {
		t.Fatal("expected controller preflight failure")
	}
	expected := desktopcontrol.ErrorText(commandErr)
	for _, op := range []string{"connect", "reconnect", "autostart"} {
		t.Run(op, func(t *testing.T) {
			s := loadedState(t)
			if !s.begin(desktopcontrol.Command{Op: op}) {
				t.Fatal("action rejected")
			}
			s.finish(desktopcontrol.Status{}, commandErr, 101)
			if s.errText != expected {
				t.Fatalf("missing command error: %q", s.errText)
			}
			for _, now := range []float64{102, 104} {
				if !s.begin(desktopcontrol.Command{Op: "status"}) {
					t.Fatal("poll rejected")
				}
				s.finish(sampleStatus(), nil, now)
				if s.errText != expected {
					t.Fatalf("healthy poll erased action error: %q", s.errText)
				}
			}
			if !s.begin(desktopcontrol.Command{Op: "reconnect"}) {
				t.Fatal("new action rejected")
			}
			if s.errText != "" {
				t.Fatalf("new action retained old error: %q", s.errText)
			}
		})
	}
}

func TestConnectivityErrorClearsAfterHealthyPoll(t *testing.T) {
	for _, op := range []string{"status", "connect", "autostart"} {
		t.Run(op, func(t *testing.T) {
			s := loadedState(t)
			s.begin(desktopcontrol.Command{Op: op})
			s.finish(desktopcontrol.Status{}, errors.New("socket unavailable"), 101)
			if s.errText == "" {
				t.Fatal("missing connectivity error")
			}
			s.begin(desktopcontrol.Command{Op: "status"})
			s.finish(sampleStatus(), nil, 102)
			if s.errText != "" {
				t.Fatalf("recovered connectivity error remains: %q", s.errText)
			}
		})
	}
}
