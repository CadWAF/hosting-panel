// Package runtimes installs and removes PHP Runtimes (parallel versions) and their extensions.
package runtimes

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/system"
	"hostingpanel/agent/internal/tasks"
)

var (
	ErrInvalid = errors.New("invalid input")
	ErrInUse   = errors.New("runtime is used by sites")

	Supported = []string{"7.4", "8.0", "8.1", "8.2", "8.3", "8.4", "8.5"}

	// Extensions installable as apt packages phpX.Y-<name>.
	Extensions = []string{"mysql", "mbstring", "xml", "curl", "zip", "gd", "intl", "bcmath", "soap", "imagick",
		"redis", "memcached", "sqlite3", "pgsql", "ldap", "gmp", "imap", "xdebug", "apcu", "mongodb", "opcache"}

	baseline = []string{"cli", "fpm", "common", "mysql", "mbstring", "xml", "curl", "zip", "gd", "intl", "opcache"}
	extRe    = regexp.MustCompile(`^[a-z0-9]+$`)
)

type Info struct {
	Version    string   `json:"version"`
	Installed  bool     `json:"installed"`
	Sites      []string `json:"sites"`
	Extensions []string `json:"extensions"`
}

type Manager struct {
	run    system.Runner
	tasks  *tasks.Manager
	st     *store.Store
	phpDir string
	apt    sync.Mutex // one apt operation at a time
}

func New(run system.Runner, t *tasks.Manager, st *store.Store, phpDir string) *Manager {
	return &Manager{run: run, tasks: t, st: st, phpDir: phpDir}
}

func supported(v string) bool {
	for _, s := range Supported {
		if s == v {
			return true
		}
	}
	return false
}

func (m *Manager) installed(v string) bool {
	_, err := os.Stat(filepath.Join(m.phpDir, v, "fpm"))
	return err == nil
}

// Installed reports whether a version is present (used by the Sites service).
func (m *Manager) Installed(v string) bool { return m.installed(v) }

func (m *Manager) sitesUsing(v string) []string {
	all, _ := m.st.List()
	out := []string{}
	for _, s := range all {
		if s.PHPVersion == v {
			out = append(out, s.Domain)
		}
	}
	return out
}

func (m *Manager) exts(v string) []string {
	out := []string{}
	files, _ := filepath.Glob(filepath.Join(m.phpDir, v, "mods-available", "*.ini"))
	for _, f := range files {
		out = append(out, strings.TrimSuffix(filepath.Base(f), ".ini"))
	}
	sort.Strings(out)
	return out
}

func (m *Manager) List() []Info {
	out := []Info{}
	for _, v := range Supported {
		i := Info{Version: v, Installed: m.installed(v), Sites: m.sitesUsing(v), Extensions: []string{}}
		if i.Installed {
			i.Extensions = m.exts(v)
		}
		out = append(out, i)
	}
	return out
}

func (m *Manager) aptRun(l *tasks.Log, args ...string) error {
	return m.run.RunW(l, "apt-get", append([]string{"-y", "-o", "Dpkg::Options::=--force-confold"}, args...)...)
}

func (m *Manager) Install(v string) (store.Task, error) {
	if !supported(v) {
		return store.Task{}, fmt.Errorf("%w: version", ErrInvalid)
	}
	return m.tasks.Start("php.install", "Install PHP "+v, func(l *tasks.Log) error {
		m.apt.Lock()
		defer m.apt.Unlock()
		l.Printf("Adding the ondrej/php repository")
		if err := m.run.RunW(l, "add-apt-repository", "-y", "ppa:ondrej/php"); err != nil {
			return err
		}
		if err := m.aptRun(l, "update"); err != nil {
			return err
		}
		pk := []string{"install"}
		for _, b := range baseline {
			pk = append(pk, "php"+v+"-"+b)
		}
		l.Printf("Installing PHP %s", v)
		if err := m.aptRun(l, pk...); err != nil {
			return err
		}
		l.Printf("Enabling php%s-fpm", v)
		return m.run.RunW(l, "systemctl", "enable", "--now", "php"+v+"-fpm")
	})
}

func (m *Manager) Remove(v string) (store.Task, error) {
	if !supported(v) {
		return store.Task{}, fmt.Errorf("%w: version", ErrInvalid)
	}
	if u := m.sitesUsing(v); len(u) > 0 {
		return store.Task{}, fmt.Errorf("%w: %s", ErrInUse, strings.Join(u, ", "))
	}
	return m.tasks.Start("php.remove", "Remove PHP "+v, func(l *tasks.Log) error {
		m.apt.Lock()
		defer m.apt.Unlock()
		_ = m.run.RunW(l, "systemctl", "disable", "--now", "php"+v+"-fpm")
		return m.aptRun(l, "purge", "php"+v+"-*")
	})
}

func (m *Manager) extPkg(v, ext string) (string, error) {
	if !m.installed(v) {
		return "", fmt.Errorf("%w: PHP %s is not installed", ErrInvalid, v)
	}
	ok := false
	for _, e := range Extensions {
		if e == ext && extRe.MatchString(ext) {
			ok = true
		}
	}
	if !ok {
		return "", fmt.Errorf("%w: extension", ErrInvalid)
	}
	return "php" + v + "-" + ext, nil
}

func (m *Manager) InstallExtension(v, ext string) (store.Task, error) {
	pkg, err := m.extPkg(v, ext)
	if err != nil {
		return store.Task{}, err
	}
	return m.tasks.Start("php.ext", "Install "+pkg, func(l *tasks.Log) error {
		m.apt.Lock()
		defer m.apt.Unlock()
		if err := m.aptRun(l, "install", pkg); err != nil {
			return err
		}
		return m.run.RunW(l, "systemctl", "reload", "php"+v+"-fpm")
	})
}

func (m *Manager) RemoveExtension(v, ext string) (store.Task, error) {
	pkg, err := m.extPkg(v, ext)
	if err != nil {
		return store.Task{}, err
	}
	return m.tasks.Start("php.ext", "Remove "+pkg, func(l *tasks.Log) error {
		m.apt.Lock()
		defer m.apt.Unlock()
		if err := m.aptRun(l, "remove", pkg); err != nil {
			return err
		}
		return m.run.RunW(l, "systemctl", "reload", "php"+v+"-fpm")
	})
}
