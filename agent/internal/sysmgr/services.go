package sysmgr

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// ServiceUnit is one systemd unit shown in the Services list.
type ServiceUnit struct {
	Unit        string `json:"unit"`
	Description string `json:"description"`
	Active      string `json:"active"` // active | inactive | failed | activating
	Sub         string `json:"sub"`
	Enabled     bool   `json:"enabled"`
	// Protected units cannot be stopped or disabled from the Panel (doing so would lock the Administrator out).
	Protected bool `json:"protected"`
}

// A unit is listed by default when it matches one of these; "all" lists every service.
var curated = regexp.MustCompile(`^(hostingpanel-.*|nginx|apache2|php[0-9.]+-fpm|mariadb|mysql|postgresql(@.*)?|redis-server|memcached|fail2ban|ufw|docker|pure-ftpd|postfix|dovecot|rspamd|supervisor|cron|ssh|chrony|clamav-.*|rabbitmq-server|unattended-upgrades|vnstat)\.service$`)

var protected = regexp.MustCompile(`^(hostingpanel-panel-fpm|hostingpanel-system|hostingpanel-core|nginx|ssh|sshd)\.service$`)

// Services lists systemd services (curated by default).
func (m *Manager) Services(all bool) []ServiceUnit {
	out := m.out("systemctl", "list-units", "--type=service", "--all", "--no-legend", "--no-pager", "--plain")
	en := m.enabledSet()
	seen := map[string]bool{}
	res := []ServiceUnit{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 4 {
			continue
		}
		u := f[0]
		if !all && !curated.MatchString(u) {
			continue
		}
		if f[1] == "not-found" {
			continue
		}
		seen[u] = true
		res = append(res, ServiceUnit{Unit: u, Description: strings.Join(f[4:], " "), Active: f[2], Sub: f[3], Enabled: en[u], Protected: protected.MatchString(u)})
	}
	sort.Slice(res, func(i, j int) bool { return res[i].Unit < res[j].Unit })
	return res
}

func (m *Manager) enabledSet() map[string]bool {
	set := map[string]bool{}
	for _, l := range strings.Split(m.out("systemctl", "list-unit-files", "--type=service", "--no-legend", "--no-pager"), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && f[1] == "enabled" {
			set[f[0]] = true
		}
	}
	return set
}

var unitActions = map[string]bool{"start": true, "stop": true, "restart": true, "reload": true, "enable": true, "disable": true}

// ServiceAction runs start/stop/restart/reload/enable/disable on a unit.
func (m *Manager) ServiceAction(unit, action string) error {
	if !unitRe.MatchString(unit) || !strings.HasSuffix(unit, ".service") {
		return invalid("unit")
	}
	if !unitActions[action] {
		return invalid("action")
	}
	if (action == "stop" || action == "disable") && protected.MatchString(unit) {
		return fmt.Errorf("%w: %s keeps the Panel and SSH reachable and cannot be stopped from here", ErrConflict, unit)
	}
	if action == "enable" || action == "disable" {
		return m.Run.Run("systemctl", action, unit)
	}
	return m.Run.Run("systemctl", action, unit)
}

// UnitLog returns recent journal lines of one unit.
func (m *Manager) UnitLog(unit string, lines int) (string, error) {
	if !unitRe.MatchString(unit) {
		return "", invalid("unit")
	}
	if lines <= 0 || lines > 2000 {
		lines = 200
	}
	out, err := m.Run.Output("journalctl", "-u", unit, "-n", fmt.Sprint(lines), "--no-pager", "-o", "short-iso")
	if err != nil && out == "" {
		return "", err
	}
	return out, nil
}
