package sysmgr

import (
	"os"
	"regexp"
	"strings"
	"time"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/tasks"
)

type Upgradable struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Current  string `json:"current"`
	Security bool   `json:"security"`
}

type Updates struct {
	Upgradable     []Upgradable `json:"upgradable"`
	Security       int          `json:"security"`
	RebootRequired bool         `json:"reboot_required"`
	LastChecked    int64        `json:"last_checked"` // unix seconds, 0 = never
	AutoUpdates    bool         `json:"auto_updates"`
}

// `apt list --upgradable` lines: name/suite version arch [upgradable from: old]
var upRe = regexp.MustCompile(`^([^/\s]+)/(\S+)\s+(\S+)\s+\S+\s+\[upgradable from: ([^\]]+)\]`)

func parseUpgradable(out string) []Upgradable {
	res := []Upgradable{}
	for _, l := range strings.Split(out, "\n") {
		g := upRe.FindStringSubmatch(l)
		if g == nil {
			continue
		}
		res = append(res, Upgradable{Name: g[1], Version: g[3], Current: g[4], Security: strings.Contains(g[2], "-security")})
	}
	return res
}

func (m *Manager) Updates() Updates {
	u := Updates{Upgradable: parseUpgradable(m.out("apt", "list", "--upgradable"))}
	for _, p := range u.Upgradable {
		if p.Security {
			u.Security++
		}
	}
	u.RebootRequired = m.exists("/var/run/reboot-required")
	if fi, err := os.Stat(m.path("/var/lib/apt/periodic/update-success-stamp")); err == nil {
		u.LastChecked = fi.ModTime().Unix()
	} else if fi, err := os.Stat(m.path("/var/lib/apt/lists")); err == nil {
		u.LastChecked = fi.ModTime().Unix()
	}
	u.AutoUpdates = len(m.dpkgState([]string{"unattended-upgrades"})) > 0
	return u
}

func (m *Manager) CheckUpdates() (store.Task, error) {
	return m.Tasks.Start("updates.check", "Check for updates", func(l *tasks.Log) error {
		m.apt.Lock()
		defer m.apt.Unlock()
		return m.Run.RunW(l, "apt-get", "update", "-q")
	})
}

func (m *Manager) ApplyUpdates() (store.Task, error) {
	return m.Tasks.Start("updates.apply", "Install updates", func(l *tasks.Log) error {
		m.apt.Lock()
		defer m.apt.Unlock()
		if err := m.Run.RunW(l, "apt-get", "update", "-q"); err != nil {
			return err
		}
		if err := m.Run.RunW(l, "apt-get", "-y", "-q", "-o", "Dpkg::Options::=--force-confold", "upgrade"); err != nil {
			return err
		}
		l.Printf("Done. A reboot may be needed for kernel updates.")
		return nil
	})
}

// Reboot restarts the server after a short delay so the HTTP reply can be delivered.
func (m *Manager) Reboot() error {
	go func() {
		time.Sleep(2 * time.Second)
		_ = m.Run.Run("systemctl", "reboot")
	}()
	return nil
}
