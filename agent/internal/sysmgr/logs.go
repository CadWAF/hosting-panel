package sysmgr

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LogSource is a log the Administrator may read. Paths are fixed here; the Panel never passes a path.
type LogSource struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Group string `json:"group"`
	Size  int64  `json:"size"`
	path  string
}

var logFiles = []LogSource{
	{ID: "syslog", Label: "System log", Group: "System", path: "/var/log/syslog"},
	{ID: "auth", Label: "Authentication (SSH logins)", Group: "System", path: "/var/log/auth.log"},
	{ID: "kern", Label: "Kernel", Group: "System", path: "/var/log/kern.log"},
	{ID: "apt", Label: "Package history (apt)", Group: "System", path: "/var/log/apt/history.log"},
	{ID: "nginx-access", Label: "nginx access", Group: "Web", path: "/var/log/nginx/access.log"},
	{ID: "nginx-error", Label: "nginx error", Group: "Web", path: "/var/log/nginx/error.log"},
	{ID: "fail2ban", Label: "Fail2ban", Group: "Security", path: "/var/log/fail2ban.log"},
	{ID: "ufw", Label: "Firewall (blocked packets)", Group: "Security", path: "/var/log/ufw.log"},
	{ID: "mysql", Label: "MariaDB / MySQL", Group: "Data", path: "/var/log/mysql/error.log"},
	{ID: "redis", Label: "Redis", Group: "Data", path: "/var/log/redis/redis-server.log"},
	{ID: "mail", Label: "Mail", Group: "Mail", path: "/var/log/mail.log"},
	{ID: "letsencrypt", Label: "Let's Encrypt", Group: "Web", path: "/var/log/letsencrypt/letsencrypt.log"},
	{ID: "panel-laravel", Label: "Panel application", Group: "Panel", path: "/opt/hostingpanel/panel/storage/logs/laravel.log"},
}

// Logs lists the logs that exist on this host.
func (m *Manager) Logs() []LogSource {
	res := []LogSource{}
	for _, s := range logFiles {
		if fi, err := os.Stat(m.path(s.path)); err == nil && !fi.IsDir() {
			s.Size = fi.Size()
			res = append(res, s)
		}
	}
	// PHP-FPM logs are named per version.
	g, _ := filepath.Glob(m.path("/var/log/php*-fpm.log"))
	sort.Strings(g)
	for _, p := range g {
		base := filepath.Base(p)
		if fi, err := os.Stat(p); err == nil {
			res = append(res, LogSource{ID: "php:" + base, Label: "PHP-FPM (" + strings.TrimSuffix(base, ".log") + ")", Group: "Web", Size: fi.Size()})
		}
	}
	return res
}

func (m *Manager) logPath(id string) (string, error) {
	if base, ok := strings.CutPrefix(id, "php:"); ok {
		if strings.ContainsAny(base, "/\\") || !strings.HasPrefix(base, "php") || !strings.HasSuffix(base, "-fpm.log") {
			return "", invalid("log")
		}
		return m.path("/var/log/" + base), nil
	}
	for _, s := range logFiles {
		if s.ID == id {
			return m.path(s.path), nil
		}
	}
	return "", ErrNotFound
}

// ReadLog returns the last `lines` lines of a log, optionally keeping only lines containing q (case-insensitive).
func (m *Manager) ReadLog(id string, lines int, q string) (string, error) {
	if lines <= 0 || lines > 2000 {
		lines = 300
	}
	if unit, ok := strings.CutPrefix(id, "unit:"); ok {
		s, err := m.UnitLog(unit, lines)
		return filterLines(s, q, lines), err
	}
	p, err := m.logPath(id)
	if err != nil {
		return "", err
	}
	f, err := os.Open(p)
	if err != nil {
		return "", ErrNotFound
	}
	defer f.Close()
	fi, _ := f.Stat()
	// Read at most the last 4 MiB; filtering can drop most lines, so look further back when q is set.
	window := int64(1 << 20)
	if q != "" {
		window = 8 << 20
	}
	off := fi.Size() - window
	if off < 0 {
		off = 0
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return "", err
	}
	b, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	s := string(b)
	if off > 0 {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
	}
	return filterLines(s, q, lines), nil
}

func filterLines(s, q string, n int) string {
	all := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if q != "" {
		q = strings.ToLower(q)
		kept := all[:0:0]
		for _, l := range all {
			if strings.Contains(strings.ToLower(l), q) {
				kept = append(kept, l)
			}
		}
		all = kept
	}
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return fmt.Sprint(strings.Join(all, "\n"))
}
