package sysmgr

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
)

type Jail struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
	Failed      int      `json:"failed"`
	TotalFailed int      `json:"total_failed"`
	Banned      int      `json:"banned"`
	TotalBanned int      `json:"total_banned"`
	IPs         []string `json:"ips"`
	BanTime     int      `json:"bantime"`
	FindTime    int      `json:"findtime"`
	MaxRetry    int      `json:"maxretry"`
}

type Fail2ban struct {
	Installed bool     `json:"installed"`
	Running   bool     `json:"running"`
	Jails     []Jail   `json:"jails"`
	Defaults  F2BDefs  `json:"defaults"`
	IgnoreIP  []string `json:"ignoreip"`
}

type F2BDefs struct {
	BanTime  int `json:"bantime"`
	FindTime int `json:"findtime"`
	MaxRetry int `json:"maxretry"`
}

// knownJails are the jails the Panel can switch on; each ships with fail2ban and only needs its log path.
var knownJails = []struct {
	Name, Desc, Extra string
}{
	{"sshd", "SSH login failures", "backend = systemd"},
	{"nginx-http-auth", "Failed HTTP basic auth in nginx", "port = http,https\nlogpath = /var/log/nginx/error.log"},
	{"nginx-botsearch", "Bots probing for missing scripts", "port = http,https\nlogpath = /var/log/nginx/access.log"},
	{"nginx-limit-req", "Requests blocked by nginx rate limits", "port = http,https\nlogpath = /var/log/nginx/error.log"},
	{"postfix", "Mail server abuse (SMTP)", "backend = systemd"},
	{"dovecot", "IMAP/POP3 login failures", "backend = systemd"},
	{"recidive", "Repeat offenders: long ban for IPs banned several times", "logpath = /var/log/fail2ban.log\nbantime = 604800\nfindtime = 86400\nmaxretry = 3"},
}

var jailRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,40}$`)

const f2bDir = "/etc/fail2ban/jail.d"

func (m *Manager) fail2banInstalled() bool {
	return len(m.dpkgState([]string{"fail2ban"})) > 0
}

// fail2banDefaults writes the Panel's baseline: sensible timings and the sshd jail on.
func (m *Manager) fail2banDefaults() error {
	if err := m.write(f2bDir+"/hostingpanel-defaults.local", "[DEFAULT]\nbantime = 3600\nfindtime = 600\nmaxretry = 5\nignoreip = 127.0.0.1/8 ::1\n", 0o644); err != nil {
		return err
	}
	return m.write(f2bDir+"/hp-sshd.local", "[sshd]\nenabled = true\nbackend = systemd\n", 0o644)
}

func ini(s string) map[string]string {
	r := map[string]string{}
	for _, l := range strings.Split(s, "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			r[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return r
}

// Fail2ban reads the live state from fail2ban-client and the Panel's own jail files.
func (m *Manager) Fail2ban() Fail2ban {
	f := Fail2ban{Jails: []Jail{}, IgnoreIP: []string{}, Defaults: F2BDefs{BanTime: 3600, FindTime: 600, MaxRetry: 5}}
	if !m.fail2banInstalled() {
		return f
	}
	f.Installed = true
	st := m.unitStates([]string{"fail2ban.service"})["fail2ban.service"]
	f.Running = st.Active
	d := ini(m.read(f2bDir + "/hostingpanel-defaults.local"))
	if v := atoi(d["bantime"]); v != 0 {
		f.Defaults.BanTime = v
	}
	if v := atoi(d["findtime"]); v != 0 {
		f.Defaults.FindTime = v
	}
	if v := atoi(d["maxretry"]); v != 0 {
		f.Defaults.MaxRetry = v
	}
	for _, ip := range strings.Fields(d["ignoreip"]) {
		if ip != "127.0.0.1/8" && ip != "::1" {
			f.IgnoreIP = append(f.IgnoreIP, ip)
		}
	}
	live := map[string]bool{}
	if f.Running {
		out := m.out("fail2ban-client", "status")
		for _, l := range strings.Split(out, "\n") {
			if _, v, ok := strings.Cut(l, "Jail list:"); ok {
				for _, n := range strings.Split(v, ",") {
					if n = strings.TrimSpace(n); n != "" {
						live[n] = true
					}
				}
			}
		}
	}
	for _, k := range knownJails {
		j := Jail{Name: k.Name, Description: k.Desc, IPs: []string{}, BanTime: f.Defaults.BanTime, FindTime: f.Defaults.FindTime, MaxRetry: f.Defaults.MaxRetry}
		if live[k.Name] {
			j.Enabled = true
			m.fillJail(&j)
		}
		f.Jails = append(f.Jails, j)
	}
	// Jails enabled outside the Panel are shown read-only-ish so nothing is hidden.
	extra := []string{}
	for n := range live {
		known := false
		for _, k := range knownJails {
			known = known || k.Name == n
		}
		if !known {
			extra = append(extra, n)
		}
	}
	sort.Strings(extra)
	for _, n := range extra {
		j := Jail{Name: n, Description: "Custom jail", Enabled: true, IPs: []string{}}
		m.fillJail(&j)
		f.Jails = append(f.Jails, j)
	}
	return f
}

func (m *Manager) fillJail(j *Jail) {
	for _, l := range strings.Split(m.out("fail2ban-client", "status", j.Name), "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch {
		case strings.HasSuffix(k, "Currently failed"):
			j.Failed = atoi(v)
		case strings.HasSuffix(k, "Total failed"):
			j.TotalFailed = atoi(v)
		case strings.HasSuffix(k, "Currently banned"):
			j.Banned = atoi(v)
		case strings.HasSuffix(k, "Total banned"):
			j.TotalBanned = atoi(v)
		case strings.HasSuffix(k, "Banned IP list"):
			j.IPs = strings.Fields(v)
		}
	}
	for _, p := range []struct {
		key string
		dst *int
	}{{"bantime", &j.BanTime}, {"findtime", &j.FindTime}, {"maxretry", &j.MaxRetry}} {
		if v := strings.TrimSpace(m.out("fail2ban-client", "get", j.Name, p.key)); v != "" {
			*p.dst = atoi(v)
		}
	}
}

type JailInput struct {
	Enabled  bool `json:"enabled"`
	BanTime  int  `json:"bantime"`
	FindTime int  `json:"findtime"`
	MaxRetry int  `json:"maxretry"`
}

// SetJail enables, disables or retunes one of the known jails.
func (m *Manager) SetJail(name string, in JailInput) error {
	var extra string
	known := false
	for _, k := range knownJails {
		if k.Name == name {
			known, extra = true, k.Extra
		}
	}
	if !known {
		return invalid("jail")
	}
	if in.BanTime < 60 || in.BanTime > 31536000 || in.FindTime < 10 || in.FindTime > 604800 || in.MaxRetry < 1 || in.MaxRetry > 100 {
		return invalid("bantime must be 60 s or more, findtime 10 s or more, maxretry 1–100")
	}
	if !m.fail2banInstalled() {
		return invalid("fail2ban is not installed")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[%s]\nenabled = %t\nbantime = %d\nfindtime = %d\nmaxretry = %d\n", name, in.Enabled, in.BanTime, in.FindTime, in.MaxRetry)
	// Keys the jail needs (log path, backend); explicit timings above win over the defaults in Extra.
	for _, l := range strings.Split(extra, "\n") {
		k, _, _ := strings.Cut(l, "=")
		switch strings.TrimSpace(k) {
		case "bantime", "findtime", "maxretry":
			continue
		}
		if l != "" {
			b.WriteString(l + "\n")
		}
	}
	if err := m.write(f2bDir+"/hp-"+name+".local", b.String(), 0o644); err != nil {
		return err
	}
	return m.Run.Run("systemctl", "reload-or-restart", "fail2ban.service")
}

type DefaultsInput struct {
	BanTime  int      `json:"bantime"`
	FindTime int      `json:"findtime"`
	MaxRetry int      `json:"maxretry"`
	IgnoreIP []string `json:"ignoreip"`
}

// SetDefaults sets the timings used by every jail and the never-ban list.
func (m *Manager) SetDefaults(in DefaultsInput) error {
	if in.BanTime < 60 || in.FindTime < 10 || in.MaxRetry < 1 || in.MaxRetry > 100 {
		return invalid("bantime must be 60 s or more, findtime 10 s or more, maxretry 1–100")
	}
	ips := []string{"127.0.0.1/8", "::1"}
	for _, ip := range in.IgnoreIP {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			continue
		}
		if _, _, err := net.ParseCIDR(ip); err != nil && net.ParseIP(ip) == nil {
			return invalid("%q is not an IP address or network", ip)
		}
		ips = append(ips, ip)
	}
	if err := m.write(f2bDir+"/hostingpanel-defaults.local",
		fmt.Sprintf("[DEFAULT]\nbantime = %d\nfindtime = %d\nmaxretry = %d\nignoreip = %s\n", in.BanTime, in.FindTime, in.MaxRetry, strings.Join(ips, " ")), 0o644); err != nil {
		return err
	}
	return m.Run.Run("systemctl", "reload-or-restart", "fail2ban.service")
}

func validIP(ip string) bool { return net.ParseIP(ip) != nil }

func (m *Manager) jailOK(name string) error {
	if !jailRe.MatchString(name) {
		return invalid("jail")
	}
	return nil
}

// Ban bans an address in a jail by hand.
func (m *Manager) Ban(jail, ip string) error {
	if err := m.jailOK(jail); err != nil {
		return err
	}
	if !validIP(ip) {
		return invalid("not an IP address")
	}
	return m.Run.Run("fail2ban-client", "set", jail, "banip", ip)
}

// Unban lifts a ban.
func (m *Manager) Unban(jail, ip string) error {
	if err := m.jailOK(jail); err != nil {
		return err
	}
	if !validIP(ip) {
		return invalid("not an IP address")
	}
	return m.Run.Run("fail2ban-client", "set", jail, "unbanip", ip)
}
