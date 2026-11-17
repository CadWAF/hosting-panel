package backup

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hostingpanel/agent/internal/system"
)

// Spec says what one backup run captures.
type Spec struct {
	Prefix  string // archive name prefix, unique per job
	HomeRel string // site home relative to "/" (e.g. srv/sites/s_x); empty = no files
	Staging string // directory holding db-*.sql.gz dumps; empty = none
}

// Backend creates, fetches and prunes backups at one Destination.
type Backend interface {
	Backup(w io.Writer, spec Spec, now time.Time) (archive string, size int64, err error)
	// Fetch extracts a backup into dir: files at dir/<HomeRel>, dumps at dir/db-*.sql.gz.
	Fetch(w io.Writer, archive, dir string) error
	Prune(w io.Writer, prefix string, keep int) ([]string, error)
	Test() error
}

type tarBackend struct {
	t   Transport
	run system.Runner
	tmp string
}

func (b *tarBackend) Backup(w io.Writer, s Spec, now time.Time) (string, int64, error) {
	name := archiveName(s.Prefix, now)
	out := filepath.Join(b.tmp, name)
	args := []string{"-czf", out}
	if s.HomeRel != "" {
		args = append(args, "-C", "/", s.HomeRel)
	}
	if s.Staging != "" {
		args = append(args, "-C", s.Staging, ".")
	}
	if len(args) == 2 {
		return "", 0, fmt.Errorf("%w: nothing to back up", ErrInvalid)
	}
	if err := b.run.RunW(w, "tar", args...); err != nil {
		return "", 0, err
	}
	st, err := os.Stat(out)
	if err != nil {
		return "", 0, err
	}
	if err := b.t.Put(name, out); err != nil {
		_ = os.Remove(out)
		return "", 0, err
	}
	return name, st.Size(), nil
}

func (b *tarBackend) Fetch(w io.Writer, archive, dir string) error {
	tmp := filepath.Join(b.tmp, archive)
	defer os.Remove(tmp)
	if err := b.t.Get(archive, tmp); err != nil {
		return err
	}
	return b.run.RunW(w, "tar", "-xzf", tmp, "-C", dir)
}

func (b *tarBackend) Prune(w io.Writer, prefix string, keep int) ([]string, error) {
	all, err := b.t.List()
	if err != nil {
		return nil, err
	}
	var gone []string
	for _, n := range pruneNames(all, prefix, keep) {
		if err := b.t.Delete(n); err != nil {
			return gone, err
		}
		fmt.Fprintf(w, "pruned %s\n", n)
		gone = append(gone, n)
	}
	return gone, nil
}

func (b *tarBackend) Test() error { return b.t.Test() }

// borgBackend uses a Borg repository (local path or ssh://user@host:port/path).
type borgBackend struct {
	repo, pass, sshKeyPath, knownHosts string
	run                                system.Runner
}

func (b *borgBackend) env() []string {
	e := []string{"BORG_PASSPHRASE=" + b.pass, "BORG_RELOCATED_REPO_ACCESS_IS_OK=no", "BORG_UNKNOWN_UNENCRYPTED_REPO_ACCESS_IS_OK=no"}
	if b.sshKeyPath != "" {
		e = append(e, "BORG_RSH=ssh -i "+b.sshKeyPath+" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile="+b.knownHosts)
	}
	return e
}

func (b *borgBackend) borg(w io.Writer, dir string, args ...string) error {
	return b.run.RunEnvW(w, b.env(), dir, "borg", args...)
}

func (b *borgBackend) ensureRepo(w io.Writer) error {
	if err := b.borg(io.Discard, "", "info", b.repo); err == nil {
		return nil
	}
	fmt.Fprintln(w, "initialising encrypted Borg repository")
	return b.borg(w, "", "init", "--encryption=repokey-blake2", b.repo)
}

func (b *borgBackend) Backup(w io.Writer, s Spec, now time.Time) (string, int64, error) {
	if err := b.ensureRepo(w); err != nil {
		return "", 0, err
	}
	name := s.Prefix + "-" + now.UTC().Format("20060102-150405")
	args := []string{"create", "--compression", "zstd", b.repo + "::" + name}
	dir := ""
	if s.Staging != "" {
		dir = s.Staging
		args = append(args, ".")
	}
	if s.HomeRel != "" {
		args = append(args, "/"+s.HomeRel)
	}
	if err := b.borg(w, dir, args...); err != nil {
		return "", 0, err
	}
	return name, 0, nil
}

func (b *borgBackend) Fetch(w io.Writer, archive, dir string) error {
	return b.borg(w, dir, "extract", b.repo+"::"+archive)
}

func (b *borgBackend) list() ([]string, error) {
	var buf bytes.Buffer
	if err := b.borg(&buf, "", "list", "--short", b.repo); err != nil {
		return nil, err
	}
	return strings.Fields(buf.String()), nil
}

func (b *borgBackend) Prune(w io.Writer, prefix string, keep int) ([]string, error) {
	before, err := b.list()
	if err != nil {
		return nil, err
	}
	if err := b.borg(w, "", "prune", "--keep-last", fmt.Sprint(keep), "--glob-archives", prefix+"-*", b.repo); err != nil {
		return nil, err
	}
	after, _ := b.list()
	have := map[string]bool{}
	for _, a := range after {
		have[a] = true
	}
	var gone []string
	for _, a := range before {
		if !have[a] {
			gone = append(gone, a)
		}
	}
	return gone, nil
}

func (b *borgBackend) Test() error {
	if err := b.run.RunEnvW(io.Discard, nil, "", "borg", "--version"); err != nil {
		return fmt.Errorf("borg is not installed: %w", err)
	}
	var out bytes.Buffer
	if err := b.borg(&out, "", "info", b.repo); err != nil {
		// A repository that does not exist yet is fine; it is created on the first backup.
		if !strings.Contains(out.String(), "does not exist") {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(out.String()))
		}
	}
	return nil
}
