// Package sysmgr is the System Service: host monitoring, installable software, firewall, fail2ban,
// systemd services, OS updates, scheduled jobs, logs, Docker and FTP accounts.
package sysmgr

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"hostingpanel/agent/internal/system"
	"hostingpanel/agent/internal/tasks"
)

var (
	ErrInvalid  = errors.New("invalid input")
	ErrNotFound = errors.New("not found")
	ErrConflict = errors.New("conflict")
)

func invalid(f string, a ...any) error { return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(f, a...)) }

// Manager owns every host-level operation of the System Service.
type Manager struct {
	Run   system.Runner
	Tasks *tasks.Manager
	// Root prefixes every file the Manager writes (development dry-run keeps the real host untouched).
	Root string
	// SitesBase is where Site homes live; FTP accounts are confined below it.
	SitesBase string
	// Lookup resolves an operating-system user (replaceable in development, where site users do not exist).
	Lookup func(name string) (uid, gid int, home string, err error)

	apt     sync.Mutex // one apt/dpkg operation at a time
	stats   *sampler
	catalog []Package
}

func New(run system.Runner, t *tasks.Manager, root string) *Manager {
	m := &Manager{Run: run, Tasks: t, Root: root, SitesBase: "/srv/sites", Lookup: osLookup}
	m.stats = newSampler()
	m.catalog = catalog()
	return m
}

func osLookup(name string) (int, int, string, error) {
	u, err := user.Lookup(name)
	if err != nil {
		return 0, 0, "", err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return uid, gid, u.HomeDir, nil
}

// path maps an absolute host path into Root.
func (m *Manager) path(p string) string { return filepath.Join(m.Root, p) }

func (m *Manager) write(p, content string, mode os.FileMode) error {
	full := m.path(p)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), mode); err != nil {
		return err
	}
	return os.Rename(tmp, full)
}

func (m *Manager) read(p string) string {
	b, _ := os.ReadFile(m.path(p))
	return string(b)
}

func (m *Manager) exists(p string) bool {
	_, err := os.Stat(m.path(p))
	return err == nil
}

func (m *Manager) out(name string, args ...string) string {
	s, _ := m.Run.Output(name, args...)
	return s
}

var (
	unitRe = regexp.MustCompile(`^[A-Za-z0-9@._-]{1,80}\.(service|socket|timer)$`)
	nameRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)
)

func fields(line string) []string { return strings.Fields(line) }

func atoi(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n }
func atof(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
