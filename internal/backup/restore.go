package backup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var BuildVersion = "development"

type RestoreTarget struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}
type RestorePlan struct {
	ID          string          `json:"id"`
	Targets     []RestoreTarget `json:"targets"`
	RollbackDir string          `json:"rollback_dir"`
	root        string
	verified    *Verified
}
type restoreStep struct {
	Destination string `json:"destination"`
	Name        string `json:"name"`
	HadOld      bool   `json:"had_old"`
	OldMoved    bool   `json:"old_moved"`
	NewMoved    bool   `json:"new_moved"`
}
type restoreJournal struct {
	Root    string        `json:"root"`
	Phase   string        `json:"phase"`
	Targets []restoreStep `json:"targets"`
}

func installationData(root string) (string, error) {
	p := filepath.Join(root, "var/lib/quic-lab")
	s, e := os.Lstat(p)
	if os.IsNotExist(e) {
		return p, nil
	}
	if e != nil {
		return "", e
	}
	if s.Mode()&os.ModeSymlink != 0 {
		resolved, e := os.Readlink(p)
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(filepath.Dir(p), resolved)
		}
		resolved = filepath.Clean(resolved)
		expected := filepath.Join(root, "var/lib/private/quic-lab")
		if e != nil || resolved != expected {
			return "", errors.New("unexpected installation symlink")
		}
		if e = checkParents(root, expected); e != nil {
			return "", e
		}
		return expected, nil
	}
	if !s.IsDir() {
		return "", errors.New("invalid installation data directory")
	}
	return p, nil
}
func installationControl(data string) (string, error) {
	data, e := filepath.Abs(data)
	if e != nil {
		return "", e
	}
	parent := filepath.Dir(data)
	if filepath.Base(data) == "quic-lab" && filepath.Base(parent) == "private" {
		parent = filepath.Dir(parent)
	}
	return filepath.Join(parent, filepath.Base(data)+"-control"), nil
}
func setRestorePending(data string, pending bool) error {
	dir, e := installationControl(data)
	if e != nil {
		return e
	}
	path := filepath.Join(dir, "restore-pending")
	if pending {
		f, e := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		e = f.Sync()
		f.Close()
		if e != nil {
			return e
		}
	} else if e = os.Remove(path); e != nil && !os.IsNotExist(e) {
		return e
	}
	return syncDirectory(dir)
}
func durableRename(src, dst string) error {
	if e := os.Rename(src, dst); e != nil {
		return e
	}
	if e := syncDirectory(filepath.Dir(src)); e != nil {
		return e
	}
	return syncDirectory(filepath.Dir(dst))
}
func syncTreeDirs(root string) error {
	dirs := []string{}
	e := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			dirs = append(dirs, path)
		}
		return nil
	})
	if e != nil {
		return e
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if e = syncDirectory(dirs[i]); e != nil {
			return e
		}
	}
	return nil
}
func checkParents(root, path string) error {
	rel, e := filepath.Rel(root, path)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("restore path outside installation")
	}
	current := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink in restore destination")
		}
	}
	return nil
}
func checkVerified(v *Verified) error {
	if v == nil {
		return errors.New("missing verified backup")
	}
	if e := validate(v.Manifest, Limits{100000, 64 << 30}); e != nil {
		return e
	}
	if v.Manifest.ServerVersion != BuildVersion {
		return errors.New("backup requires the same server version")
	}
	root, e := os.OpenRoot(v.Dir)
	if e != nil {
		return e
	}
	defer root.Close()
	for _, entry := range v.Manifest.Entries {
		if !strings.HasPrefix(entry.Path, "data/") && !strings.HasPrefix(entry.Path, "config/") {
			return errors.New("unknown restore component")
		}
		info, e := root.Lstat(entry.Path)
		if e != nil || !info.Mode().IsRegular() || info.Size() != entry.Size {
			return errors.New("backup staging changed")
		}
		f, e := root.Open(entry.Path)
		if e != nil {
			return e
		}
		hash := sha256.New()
		_, e = io.Copy(hash, f)
		f.Close()
		if e != nil || hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
			return errors.New("backup staging checksum mismatch")
		}
	}
	return nil
}
func PlanRestore(v *Verified, installRoot string) (RestorePlan, error) {
	var out RestorePlan
	if e := checkVerified(v); e != nil {
		return out, e
	}
	root, e := filepath.Abs(installRoot)
	if e != nil {
		return out, e
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		return out, e
	}
	data, e := installationData(root)
	if e != nil {
		return out, e
	}
	var id [16]byte
	if _, e = rand.Read(id[:]); e != nil {
		return out, e
	}
	out = RestorePlan{ID: hex.EncodeToString(id[:]), root: root, verified: v}
	out.RollbackDir = filepath.Join(root, "var/lib/quic-lab-restore", out.ID)
	seen := map[string]bool{}
	for _, entry := range v.Manifest.Entries {
		seen[strings.SplitN(entry.Path, "/", 2)[0]] = true
	}
	for _, component := range []string{"config", "data"} {
		if !seen[component] {
			continue
		}
		dest := filepath.Join(root, "etc/quic-lab")
		if component == "data" {
			dest = data
		}
		if e = checkParents(root, dest); e != nil {
			return RestorePlan{}, e
		}
		out.Targets = append(out.Targets, RestoreTarget{Source: filepath.Join(v.Dir, component), Destination: dest})
	}
	return out, nil
}
func persistJournal(dir string, j restoreJournal) error {
	raw, e := json.MarshalIndent(j, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".journal-")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(raw)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = os.Rename(f.Name(), filepath.Join(dir, "journal.json"))
	}
	if e != nil {
		return e
	}
	return syncDirectory(dir)
}
func syncDirectory(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func restoreLocks(data string) ([]*os.File, error) {
	if _, e := os.Stat(data); os.IsNotExist(e) {
		return nil, nil
	} else if e != nil {
		return nil, e
	}
	files := []*os.File{}
	for _, name := range []string{".server-owner.lock", ".awg-worker.lock", ".awg-admission.lock", ".vless-admission.sock.lock", ".transit-worker.lock"} {
		if _, e := os.Lstat(filepath.Join(data, name)); os.IsNotExist(e) {
			continue
		} else if e != nil {
			return nil, e
		}
		f, e := AcquireProcessLock(data, name)
		if e != nil {
			for _, f := range files {
				f.Close()
			}
			return nil, errors.New("stop server and workers before restore")
		}
		files = append(files, f)
	}
	vless := filepath.Join(data, "vless")
	if _, e := os.Stat(filepath.Join(vless, ".vless-control.sock.lock")); e == nil {
		f, e := AcquireProcessLock(vless, ".vless-control.sock.lock")
		if e != nil {
			for _, f := range files {
				f.Close()
			}
			return nil, errors.New("stop VLESS worker before restore")
		}
		files = append(files, f)
	}
	return files, nil
}
func ApplyRestore(ctx context.Context, p RestorePlan) error {
	if p.verified == nil || p.root == "" || !jobIDPattern.MatchString(p.ID) || p.RollbackDir != filepath.Join(p.root, "var/lib/quic-lab-restore", p.ID) {
		return errors.New("invalid restore plan")
	}
	if e := checkVerified(p.verified); e != nil {
		return e
	}
	gate, e := AcquireInstallationLock(filepath.Join(p.root, "var/lib/quic-lab"), true)
	if e != nil {
		return e
	}
	defer gate.Close()
	data, e := installationData(p.root)
	if e != nil {
		return e
	}
	if e = checkParents(p.root, p.RollbackDir); e != nil {
		return e
	}
	locks, e := restoreLocks(data)
	if e != nil {
		return e
	}
	defer func() {
		for _, f := range locks {
			f.Close()
		}
	}()
	base := filepath.Dir(p.RollbackDir)
	existing, _ := os.ReadDir(base)
	for _, entry := range existing {
		if !entry.IsDir() || !jobIDPattern.MatchString(entry.Name()) {
			continue
		}
		raw, e := os.ReadFile(filepath.Join(base, entry.Name(), "journal.json"))
		if e != nil {
			return errors.New("unresolved restore journal")
		}
		var j restoreJournal
		if json.Unmarshal(raw, &j) != nil || j.Phase != "complete" && j.Phase != "rolled_back" {
			return errors.New("previous restore requires rollback")
		}
	}
	if e = os.MkdirAll(base, 0700); e != nil {
		return e
	}
	// Publish the first journal atomically; an unpublished preparation never
	// contains moved installation data and cannot block a later transaction.
	prep, e := os.MkdirTemp(base, ".prepare-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(prep)
	for _, name := range []string{"old", "new"} {
		if e = os.Mkdir(filepath.Join(prep, name), 0700); e != nil {
			return e
		}
	}
	j := restoreJournal{Root: p.root, Phase: "preparing"}
	for _, target := range p.Targets {
		name := filepath.Base(target.Source)
		want := filepath.Join(p.root, "etc/quic-lab")
		if name == "data" {
			want = data
		} else if name != "config" {
			return errors.New("unknown restore component")
		}
		if target.Destination != want || target.Source != filepath.Join(p.verified.Dir, name) {
			return errors.New("restore map changed")
		}
		if e = checkParents(p.root, want); e != nil {
			return e
		}
		_, e = os.Stat(want)
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		j.Targets = append(j.Targets, restoreStep{Destination: want, Name: name, HadOld: e == nil})
		if e = os.MkdirAll(filepath.Join(prep, "new", name), 0700); e != nil {
			return e
		}
	}
	if e = persistJournal(prep, j); e != nil {
		return e
	}
	if e = syncTreeDirs(prep); e != nil {
		return e
	}
	if e = durableRename(prep, p.RollbackDir); e != nil {
		return e
	}
	if e = syncDirectory(filepath.Dir(base)); e != nil {
		return e
	}
	for _, entry := range p.verified.Manifest.Entries {
		if e = ctx.Err(); e != nil {
			return e
		}
		src := filepath.Join(p.verified.Dir, entry.Path)
		dst := filepath.Join(p.RollbackDir, "new", entry.Path)
		if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
			return e
		}
		in, e := os.Open(src)
		if e != nil {
			return e
		}
		out, e := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			in.Close()
			return e
		}
		hash := sha256.New()
		n, e := io.Copy(io.MultiWriter(out, hash), contextReader{ctx, in})
		in.Close()
		if e == nil && (n != entry.Size || hex.EncodeToString(hash.Sum(nil)) != entry.SHA256) {
			e = errors.New("restore source changed")
		}
		if e == nil {
			e = out.Sync()
		}
		ce := out.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			return e
		}
	}
	// Carry lock inodes into the replacement so restarted services cannot race it.
	for _, f := range locks {
		rel, e := filepath.Rel(data, f.Name())
		if e != nil {
			return e
		}
		dest := filepath.Join(p.RollbackDir, "new/data", rel)
		if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return e
		}
		if e = os.Link(f.Name(), dest); e != nil {
			return e
		}
	}
	if _, e = os.Stat(data); e == nil {
		if e = copyOwnership(data, filepath.Join(p.RollbackDir, "new/data")); e != nil {
			return e
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if e = syncTreeDirs(p.RollbackDir); e != nil {
		return e
	}
	if e = setRestorePending(data, true); e != nil {
		return e
	}

	j.Phase = "applying"
	if e = persistJournal(p.RollbackDir, j); e != nil {
		return e
	}
	for i := range j.Targets {
		step := &j.Targets[i]
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = os.MkdirAll(filepath.Dir(step.Destination), 0700); e != nil {
			return e
		}
		if step.HadOld {
			if e = durableRename(step.Destination, filepath.Join(p.RollbackDir, "old", step.Name)); e != nil {
				return e
			}
			step.OldMoved = true
			if e = persistJournal(p.RollbackDir, j); e != nil {
				return e
			}
		}
		if e = durableRename(filepath.Join(p.RollbackDir, "new", step.Name), step.Destination); e != nil {
			return e
		}
		step.NewMoved = true
		if e = syncDirectory(filepath.Dir(step.Destination)); e != nil {
			return e
		}
		if e = persistJournal(p.RollbackDir, j); e != nil {
			return e
		}
	}
	j.Phase = "complete"
	if e = persistJournal(p.RollbackDir, j); e != nil {
		return e
	}
	return setRestorePending(data, false)
}
func RollbackRestore(ctx context.Context, journalPath string) error {
	raw, e := os.ReadFile(journalPath)
	if e != nil || len(raw) > 1<<20 {
		return errors.New("invalid restore journal")
	}
	var j restoreJournal
	if json.Unmarshal(raw, &j) != nil {
		return errors.New("invalid restore journal")
	}
	dir := filepath.Dir(journalPath)
	if !filepath.IsAbs(j.Root) || dir != filepath.Join(j.Root, "var/lib/quic-lab-restore", filepath.Base(dir)) || !jobIDPattern.MatchString(filepath.Base(dir)) {
		return errors.New("invalid rollback location")
	}
	if e = checkParents(j.Root, dir); e != nil {
		return e
	}
	gate, e := AcquireInstallationLock(filepath.Join(j.Root, "var/lib/quic-lab"), true)
	if e != nil {
		return e
	}
	defer gate.Close()
	data, e := installationData(j.Root)
	if e != nil {
		return e
	}
	locks, e := restoreLocks(data)
	if e != nil {
		return e
	}
	defer func() {
		for _, f := range locks {
			f.Close()
		}
	}()
	if j.Phase == "rolled_back" {
		return setRestorePending(data, false)
	}
	seen := map[string]bool{}
	for _, step := range j.Targets {
		want := filepath.Join(j.Root, "etc/quic-lab")
		if step.Name == "data" {
			want = data
		} else if step.Name != "config" {
			return errors.New("unknown rollback target")
		}
		if step.Destination != want || seen[step.Name] {
			return errors.New("invalid rollback map")
		}
		seen[step.Name] = true
		if e = checkParents(j.Root, want); e != nil {
			return e
		}
	}
	failed := filepath.Join(dir, "failed")
	if e = os.MkdirAll(failed, 0700); e != nil {
		return e
	}
	if e = syncDirectory(dir); e != nil {
		return e
	}
	if e = setRestorePending(data, true); e != nil {
		return e
	}
	j.Phase = "rolling_back"
	if e = persistJournal(dir, j); e != nil {
		return e
	}
	for i := len(j.Targets) - 1; i >= 0; i-- {
		if e = ctx.Err(); e != nil {
			return e
		}
		step := j.Targets[i]
		old := filepath.Join(dir, "old", step.Name)
		_, oldErr := os.Stat(old)
		_, newErr := os.Stat(filepath.Join(dir, "new", step.Name))
		_, targetErr := os.Stat(step.Destination)
		shouldRestore := oldErr == nil
		shouldRemove := !step.HadOld && os.IsNotExist(newErr)
		if shouldRestore || shouldRemove {
			if targetErr == nil {
				if _, e = os.Stat(filepath.Join(failed, step.Name)); !os.IsNotExist(e) {
					// systemd can recreate an empty StateDirectory before the binary
					// observes restore-pending. Never discard nonempty data.
					if e = os.Remove(step.Destination); e != nil {
						return fmt.Errorf("rollback destination occupied: %s", step.Name)
					}
					if e = syncDirectory(filepath.Dir(step.Destination)); e != nil {
						return e
					}
					targetErr = os.ErrNotExist
				}
				if targetErr == nil {
					if e = durableRename(step.Destination, filepath.Join(failed, step.Name)); e != nil {
						return e
					}
				}
			}
			if shouldRestore {
				if e = durableRename(old, step.Destination); e != nil {
					return e
				}
			}
			if e = syncDirectory(filepath.Dir(step.Destination)); e != nil {
				return e
			}
		}
	}
	j.Phase = "rolled_back"
	if e = persistJournal(dir, j); e != nil {
		return e
	}
	return setRestorePending(data, false)
}
