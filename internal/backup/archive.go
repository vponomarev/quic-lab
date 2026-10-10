// Package backup provides authenticated, bounded server backup containers.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"filippo.io/age"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type Kind string

const (
	Config Kind = "config"
	Full   Kind = "full"
)

type Entry struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	FormatVersion int       `json:"format_version"`
	Kind          Kind      `json:"type"`
	CreatedAt     time.Time `json:"created_at"`
	ServerVersion string    `json:"server_version"`
	Components    []string  `json:"components"`
	Entries       []Entry   `json:"entries"`
}
type Limits struct {
	MaxFiles int
	MaxBytes int64
}
type Snapshot struct {
	Dir      string
	Manifest Manifest
	Release  func() error
}
type Verified struct {
	Dir      string
	Manifest Manifest
	Release  func() error
}

func safePath(s string) bool {
	return s != "" && s != "." && !strings.ContainsAny(s, "\\:\x00") && !path.IsAbs(s) && path.Clean(s) == s && s != ".." && !strings.HasPrefix(s, "../") && s != "manifest.json"
}
func validate(m Manifest, limits Limits) error {
	if limits.MaxFiles <= 0 || limits.MaxFiles > 1000000 || limits.MaxBytes <= 0 || limits.MaxBytes > 1<<50 {
		return errors.New("invalid backup limits")
	}
	if m.FormatVersion != 1 || (m.Kind != Config && m.Kind != Full) || m.CreatedAt.IsZero() || m.ServerVersion == "" || len(m.Components) == 0 || len(m.Entries) == 0 || len(m.Entries) > limits.MaxFiles {
		return errors.New("invalid backup manifest")
	}
	names := map[string]bool{}
	var total int64
	for _, e := range m.Entries {
		h, err := hex.DecodeString(e.SHA256)
		if !safePath(e.Path) || names[e.Path] || e.Size < 0 || e.Size > limits.MaxBytes-total || err != nil || len(h) != 32 {
			return errors.New("invalid backup entry")
		}
		names[e.Path] = true
		total += e.Size
	}
	return nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// Write only accepts pinned immutable input. It checks content hashes again while
// copying so a changed snapshot cannot produce a falsely successful backup.
func Write(ctx context.Context, dst io.Writer, s Snapshot, password []byte) (err error) {
	if len(password) == 0 {
		return errors.New("empty backup password")
	}
	if err = validate(s.Manifest, Limits{1000000, 1 << 50}); err != nil {
		return err
	}
	raw, err := json.Marshal(s.Manifest)
	if err != nil {
		return err
	}
	if len(raw) > 4<<20 {
		return errors.New("manifest too large")
	}
	recipient, err := age.NewScryptRecipient(string(password))
	if err != nil {
		return err
	}
	encrypted, err := age.Encrypt(dst, recipient)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(encrypted)
	tw := tar.NewWriter(gz)
	root, err := os.OpenRoot(s.Dir)
	if err != nil {
		return err
	}
	defer root.Close()
	if err = tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0600, Size: int64(len(raw)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err = tw.Write(raw); err != nil {
		return err
	}
	for _, e := range s.Manifest.Entries {
		info, x := root.Lstat(e.Path)
		if x != nil {
			return x
		}
		if !info.Mode().IsRegular() || info.Size() != e.Size {
			return errors.New("snapshot entry changed")
		}
		f, x := root.Open(e.Path)
		if x != nil {
			return x
		}
		x = tw.WriteHeader(&tar.Header{Name: e.Path, Mode: 0600, Size: e.Size, Typeflag: tar.TypeReg})
		h := sha256.New()
		var n int64
		if x == nil {
			n, x = io.Copy(io.MultiWriter(tw, h), contextReader{ctx, f})
		}
		ce := f.Close()
		if x != nil {
			return x
		}
		if ce != nil {
			return ce
		}
		if n != e.Size || hex.EncodeToString(h.Sum(nil)) != e.SHA256 {
			return errors.New("snapshot checksum changed")
		}
	}
	if err = tw.Close(); err != nil {
		return err
	}
	if err = gz.Close(); err != nil {
		return err
	}
	return encrypted.Close()
}

// Verify reads the complete authenticated age stream before returning any data.
// Component-specific schema and cross-reference checks run before restore.
func Verify(ctx context.Context, src io.Reader, password []byte, staging string, limits Limits) (result *Verified, err error) {
	if limits.MaxFiles <= 0 || limits.MaxFiles > 1000000 || limits.MaxBytes <= 0 || limits.MaxBytes > 1<<50 {
		return nil, errors.New("invalid backup limits")
	}
	identity, err := age.NewScryptIdentity(string(password))
	if err != nil {
		return nil, errors.New("invalid backup password")
	}
	identity.SetMaxWorkFactor(18)
	plain, err := age.Decrypt(contextReader{ctx, src}, identity)
	if err != nil {
		return nil, errors.New("cannot decrypt backup")
	}
	gz, err := gzip.NewReader(plain)
	if err != nil {
		return nil, errors.New("invalid compressed backup")
	}
	defer gz.Close()
	// Includes all decompressed bytes, including tar padding and trailing members.
	bounded := &io.LimitedReader{R: contextReader{ctx, gz}, N: limits.MaxBytes + int64(limits.MaxFiles)*1024 + (8 << 20)}
	tr := tar.NewReader(bounded)
	head, err := tr.Next()
	if err != nil {
		return nil, err
	}
	if head.Name != "manifest.json" || head.Typeflag != tar.TypeReg || head.Size <= 0 || head.Size > 4<<20 {
		return nil, errors.New("missing or invalid manifest")
	}
	raw, err := io.ReadAll(tr)
	if err != nil {
		return nil, err
	}
	var m Manifest
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err = dec.Decode(&m); err != nil {
		return nil, errors.New("invalid manifest JSON")
	}
	if dec.Decode(new(any)) != io.EOF {
		return nil, errors.New("trailing manifest JSON")
	}
	if err = validate(m, limits); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(staging, ".backup-verify-")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dir)
		}
	}()
	expected := map[string]Entry{}
	for _, e := range m.Entries {
		expected[e.Path] = e
	}
	seen := map[string]bool{}
	for {
		h, x := tr.Next()
		if x == io.EOF {
			break
		}
		if x != nil {
			return nil, x
		}
		e, ok := expected[h.Name]
		if !ok || seen[h.Name] || !safePath(h.Name) || h.Typeflag != tar.TypeReg || h.Size != e.Size {
			return nil, errors.New("unexpected backup entry")
		}
		seen[h.Name] = true
		name := filepath.Join(dir, filepath.FromSlash(h.Name))
		if err = os.MkdirAll(filepath.Dir(name), 0700); err != nil {
			return nil, err
		}
		f, x := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if x != nil {
			return nil, x
		}
		hash := sha256.New()
		n, x := io.Copy(io.MultiWriter(f, hash), tr)
		ce := f.Close()
		if x != nil {
			return nil, x
		}
		if ce != nil {
			return nil, ce
		}
		if n != e.Size || hex.EncodeToString(hash.Sum(nil)) != e.SHA256 {
			return nil, errors.New("backup checksum mismatch")
		}
	}
	if len(seen) != len(expected) {
		return nil, errors.New("missing backup entries")
	}
	// tar.Reader stops at end markers. Consume all remaining gzip/age data so an
	// unauthenticated tail cannot be mistaken for a verified archive.
	tail, err := io.Copy(io.Discard, bounded)
	if err != nil {
		return nil, err
	}
	if bounded.N == 0 {
		return nil, errors.New("expanded backup limit exceeded")
	}
	if tail != 0 {
		return nil, errors.New("trailing archive data")
	}
	if _, err = io.Copy(io.Discard, plain); err != nil {
		return nil, fmt.Errorf("backup authentication: %w", err)
	}
	return &Verified{Dir: dir, Manifest: m, Release: func() error { return os.RemoveAll(dir) }}, nil
}
