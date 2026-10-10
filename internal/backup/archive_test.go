package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"filippo.io/age"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func fixture(t *testing.T, kind Kind) Snapshot {
	t.Helper()
	dir := t.TempDir()
	data := []byte("private-key-data\n")
	h := sha256.Sum256(data)
	if err := os.WriteFile(filepath.Join(dir, "identities.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return Snapshot{Dir: dir, Manifest: Manifest{FormatVersion: 1, Kind: kind, CreatedAt: time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), ServerVersion: "test", Components: []string{"identities"}, Entries: []Entry{{Path: "identities.json", Size: int64(len(data)), SHA256: hex.EncodeToString(h[:])}}}}
}
func TestArchiveRoundTrip(t *testing.T) {
	for _, kind := range []Kind{Config, Full} {
		t.Run(string(kind), func(t *testing.T) {
			s := fixture(t, kind)
			var out bytes.Buffer
			if err := Write(context.Background(), &out, s, []byte("correct horse battery staple")); err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(out.Bytes(), []byte("private-key-data")) {
				t.Fatal("plaintext leaked")
			}
			v, err := Verify(context.Background(), bytes.NewReader(out.Bytes()), []byte("correct horse battery staple"), t.TempDir(), Limits{MaxFiles: 10, MaxBytes: 1 << 20})
			if err != nil {
				t.Fatal(err)
			}
			defer v.Release()
			data, err := os.ReadFile(filepath.Join(v.Dir, "identities.json"))
			if err != nil || string(data) != "private-key-data\n" || v.Manifest.Kind != kind {
				t.Fatalf("bad restored data: %q %v", data, err)
			}
		})
	}
}

// Tests use independently built age/tar input, not the writer under test.
func encryptedArchive(t *testing.T, headers []*tar.Header, bodies [][]byte) []byte {
	t.Helper()
	var out bytes.Buffer
	r, err := age.NewScryptRecipient("pass")
	if err != nil {
		t.Fatal(err)
	}
	r.SetWorkFactor(10)
	a, err := age.Encrypt(&out, r)
	if err != nil {
		t.Fatal(err)
	}
	g := gzip.NewWriter(a)
	w := tar.NewWriter(g)
	for i, h := range headers {
		if err = w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if len(bodies[i]) > 0 {
			if _, err = w.Write(bodies[i]); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, c := range []io.Closer{w, g, a} {
		if err = c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return out.Bytes()
}
func TestArchiveRejectUnsafeEntries(t *testing.T) {
	s := fixture(t, Config)
	m, _ := json.Marshal(s.Manifest)
	tests := []struct {
		name string
		h    *tar.Header
		body []byte
	}{
		{"traversal", &tar.Header{Name: "../escape", Mode: 0600, Size: 1}, []byte("x")},
		{"absolute", &tar.Header{Name: "/escape", Mode: 0600, Size: 1}, []byte("x")},
		{"backslash", &tar.Header{Name: `a\b`, Mode: 0600, Size: 1}, []byte("x")},
		{"symlink", &tar.Header{Name: "identities.json", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}, nil},
		{"hardlink", &tar.Header{Name: "identities.json", Typeflag: tar.TypeLink, Linkname: "/etc/passwd"}, nil},
		{"fifo", &tar.Header{Name: "identities.json", Typeflag: tar.TypeFifo}, nil},
		{"duplicate", &tar.Header{Name: "manifest.json", Size: int64(len(m))}, m},
		{"wrong_hash", &tar.Header{Name: "identities.json", Size: 17}, []byte("wrong-secret-data")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw := encryptedArchive(t, []*tar.Header{{Name: "manifest.json", Size: int64(len(m)), Mode: 0600}, tt.h}, [][]byte{m, tt.body})
			dir := t.TempDir()
			v, err := Verify(context.Background(), bytes.NewReader(raw), []byte("pass"), dir, Limits{MaxFiles: 10, MaxBytes: 1 << 20})
			if err == nil {
				v.Release()
				t.Fatal("unsafe archive accepted")
			}
			files, _ := os.ReadDir(dir)
			if len(files) != 0 {
				t.Fatal("staging leaked")
			}
		})
	}
}
func TestArchiveAuthenticationBeforeSuccess(t *testing.T) {
	s := fixture(t, Config)
	m, _ := json.Marshal(s.Manifest)
	data := []byte("private-key-data\n")
	raw := encryptedArchive(t, []*tar.Header{{Name: "manifest.json", Size: int64(len(m))}, {Name: "identities.json", Size: int64(len(data))}}, [][]byte{m, data})
	bad := append([]byte(nil), raw...)
	bad[len(bad)-1] ^= 1
	cases := []struct {
		name   string
		data   []byte
		pass   string
		limits Limits
	}{
		{"wrong-password", raw, "wrong", Limits{10, 1 << 20}}, {"truncated", raw[:len(raw)-1], "pass", Limits{10, 1 << 20}}, {"tampered", bad, "pass", Limits{10, 1 << 20}}, {"size-limit", raw, "pass", Limits{10, 10}}, {"count-limit", raw, "pass", Limits{0, 1 << 20}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			v, err := Verify(context.Background(), bytes.NewReader(tt.data), []byte(tt.pass), dir, tt.limits)
			if err == nil {
				v.Release()
				t.Fatal("invalid input accepted")
			}
			f, _ := os.ReadDir(dir)
			if len(f) != 0 {
				t.Fatal("staging remains")
			}
		})
	}
}
