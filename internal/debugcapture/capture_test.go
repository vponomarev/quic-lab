package debugcapture

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"strings"
	"testing"
	"time"
)

func testSession() *Session {
	ctx, cancel := context.WithCancel(context.Background())
	return &Session{Info: Info{ID: "id", Owner: "teacher", Port: 45400, State: "running", Until: time.Now().Add(time.Minute)}, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1)}
}
func TestLaunchIsolationAndReplay(t *testing.T) {
	s := testSession()
	defer s.cancel()
	m := &Manager{sessions: map[string]*Session{s.ID: s}}
	if m.Get(s.ID, "other") != nil {
		t.Fatal("cross-teacher leak")
	}
	ticket, e := s.Ticket()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Redeem(s.ID, "wrong"); e == nil {
		t.Fatal("accepted invalid ticket")
	}
	token, e := m.Redeem(s.ID, ticket)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Redeem(s.ID, ticket); e == nil {
		t.Fatal("ticket replay")
	}
	if m.Attach(s.ID, token) != s || m.Attach(s.ID, token) != nil {
		t.Fatal("stream replay")
	}
	m.StopOwner("teacher")
	if s.Snapshot().State == "running" {
		t.Fatal("logout did not stop capture")
	}
}
func TestLimitAndLateKeyLog(t *testing.T) {
	s := testSession()
	defer s.cancel()
	s.Bytes = MaxBytes - 1
	if s.emit([]byte{1, 2}) == nil {
		t.Fatal("limit ignored")
	}
	if n, e := s.Write([]byte("secret")); n != 6 || e != nil {
		t.Fatal("stopping debug must not break TLS")
	}
	if len(s.blocks) != 0 {
		t.Fatal("stored secrets after stop")
	}
}
func TestPCAPConversionAndSecrets(t *testing.T) {
	var input bytes.Buffer
	h := make([]byte, 24)
	le.PutUint32(h, 0xa1b2c3d4)
	le.PutUint16(h[4:], 2)
	le.PutUint16(h[6:], 4)
	le.PutUint32(h[16:], 65535)
	le.PutUint32(h[20:], 101)
	input.Write(h)
	packet := []byte{0x45, 0, 0, 20}
	h = make([]byte, 16)
	le.PutUint32(h, 123)
	le.PutUint32(h[4:], 456)
	le.PutUint32(h[8:], 4)
	le.PutUint32(h[12:], 4)
	input.Write(h)
	input.Write(packet)
	var blocks [][]byte
	ready := make(chan error, 1)
	e := readPCAP(&input, func(b []byte) error { blocks = append(blocks, b); return nil }, ready)
	if e != io.EOF || <-ready != nil || len(blocks) != 2 {
		t.Fatal(e)
	}
	if binary.LittleEndian.Uint32(blocks[1]) != 6 || le.Uint32(blocks[1][16:]) != 123000456 {
		t.Fatal("bad timestamp")
	}
	dsb := secrets([]byte("CLIENT_HANDSHAKE_TRAFFIC_SECRET test\n"))
	if le.Uint32(dsb) != 10 || le.Uint32(dsb[8:]) != 0x544c534b || int(le.Uint32(dsb[4:])) != len(dsb) {
		t.Fatal("invalid DSB")
	}
	ready = make(chan error, 1)
	if readPCAP(bytes.NewReader([]byte("not pcap")), func([]byte) error { return nil }, ready) == nil || <-ready == nil {
		t.Fatal("invalid pcap accepted")
	}
}

func TestUploadAuthorizationAndStreamExclusion(t *testing.T) {
	s := testSession()
	defer s.cancel()
	s.uploadToken = "upload"
	m := &Manager{sessions: map[string]*Session{s.ID: s}, excluded: map[string]int{}}
	key := []byte("CLIENT_TRAFFIC_SECRET_0 " + strings.Repeat("a", 64) + " " + strings.Repeat("b", 64) + "\n")
	if m.Upload(s.ID, "wrong", key) || m.Upload(s.ID, "upload", []byte("PRIVATE KEY")) {
		t.Fatal("invalid upload accepted")
	}
	if !m.Upload(s.ID, "upload", key) || len(s.Keys()) == 0 {
		t.Fatal("key upload")
	}
	packet := make([]byte, 16+40)
	packet[16] = 0x45
	packet[25] = 6
	copy(packet[28:32], []byte{192, 0, 2, 10})
	copy(packet[32:36], []byte{192, 0, 2, 20})
	packet[36] = 0x01
	packet[37] = 0xbb
	packet[38] = 0xc3
	packet[39] = 0x50
	body := make([]byte, 20+len(packet))
	copy(body[20:], packet)
	b := block(6, body)
	if m.isExcluded(b, 113) {
		t.Fatal("normal data excluded")
	}
	release := m.Exclude("192.0.2.20:50000")
	defer release()
	if !m.isExcluded(b, 113) {
		t.Fatal("capture feedback loop not excluded")
	}
	s.Stop("done")
	if m.Upload(s.ID, "upload", key) {
		t.Fatal("upload accepted after stop")
	}
}

func TestEnrollmentIsOneTimeAndSessionBound(t *testing.T) {
	s := &Session{Info: Info{State: "running", Until: time.Now().Add(time.Minute)}}
	m := &Manager{sessions: map[string]*Session{"test": s}}
	token, err := s.Enrollment()
	if err != nil || len(token) != 48 {
		t.Fatal("enrollment", err)
	}
	if m.Enroll("bad") != nil {
		t.Fatal("accepted invalid token")
	}
	if m.Enroll(token) != s || m.Enroll(token) != nil {
		t.Fatal("one-time enrollment failed")
	}
	old, _ := s.Enrollment()
	current, _ := s.Enrollment()
	if m.Enroll(old) != nil {
		t.Fatal("replaced QR remains valid")
	}
	s.enrollmentUntil = time.Now().Add(-time.Second)
	if m.Enroll(current) != nil {
		t.Fatal("expired QR accepted")
	}
	current, _ = s.Enrollment()
	s.State = "stopped"
	if m.Enroll(current) != nil {
		t.Fatal("stopped session accepted")
	}
}
