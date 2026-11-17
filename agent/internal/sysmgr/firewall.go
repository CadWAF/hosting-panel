package sysmgr

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

type FWRule struct {
	N       int    `json:"n"`
	To      string `json:"to"`
	Action  string `json:"action"`
	From    string `json:"from"`
	Comment string `json:"comment"`
	V6      bool   `json:"v6"`
}

type Listener struct {
	Proto   string `json:"proto"`
	Addr    string `json:"addr"`
	Port    int    `json:"port"`
	Process string `json:"process"`
	Public  bool   `json:"public"`
}

type Firewall struct {
	Installed  bool       `json:"installed"`
	Active     bool       `json:"active"`
	DefaultIn  string     `json:"default_in"`
	DefaultOut string     `json:"default_out"`
	Rules      []FWRule   `json:"rules"`
	Listening  []Listener `json:"listening"`
	SSHPort    int        `json:"ssh_port"`
}

var ruleRe = regexp.MustCompile(`^\[\s*(\d+)\]\s+(.+?)\s{2,}(ALLOW|DENY|REJECT|LIMIT)(?: (?:IN|OUT|FWD))?\s+(.+?)(?:\s+# (.*))?$`)

// parseRules reads `ufw status numbered`.
func parseRules(out string) []FWRule {
	rules := []FWRule{}
	for _, l := range strings.Split(out, "\n") {
		g := ruleRe.FindStringSubmatch(strings.TrimRight(l, " "))
		if g == nil {
			continue
		}
		r := FWRule{N: atoi(g[1]), To: strings.TrimSpace(g[2]), Action: g[3], From: strings.TrimSpace(g[4]), Comment: g[5]}
		if strings.Contains(r.To, "(v6)") {
			r.V6, r.To = true, strings.TrimSpace(strings.Replace(r.To, "(v6)", "", 1))
		}
		r.From = strings.TrimSpace(strings.Replace(r.From, "(v6)", "", 1))
		rules = append(rules, r)
	}
	return rules
}

var procRe = regexp.MustCompile(`\(\("([^"]+)"`)

// listeners parses `ss -H -tulnp`.
func listeners(out string) []Listener {
	res := []Listener{}
	seen := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 5 {
			continue
		}
		local := f[4]
		i := strings.LastIndex(local, ":")
		if i < 0 {
			continue
		}
		addr, port := strings.Trim(local[:i], "[]"), atoi(local[i+1:])
		proc := ""
		if g := procRe.FindStringSubmatch(l); g != nil {
			proc = g[1]
		}
		k := f[0] + addr + strconv.Itoa(port)
		if seen[k] {
			continue
		}
		seen[k] = true
		pub := addr == "0.0.0.0" || addr == "*" || addr == "::" || (net.ParseIP(addr) != nil && !net.ParseIP(addr).IsLoopback())
		res = append(res, Listener{Proto: f[0], Addr: addr, Port: port, Process: proc, Public: pub})
	}
	return res
}

func (m *Manager) sshPort() int {
	for _, l := range strings.Split(m.out("sshd", "-T"), "\n") {
		if v, ok := strings.CutPrefix(l, "port "); ok {
			return atoi(v)
		}
	}
	return 22
}

func (m *Manager) ufwInstalled() bool { return len(m.dpkgState([]string{"ufw"})) > 0 }

// Firewall reports ufw state, rules and what is listening on the network.
func (m *Manager) Firewall() Firewall {
	f := Firewall{Rules: []FWRule{}, SSHPort: m.sshPort(), DefaultIn: "deny", DefaultOut: "allow"}
	f.Listening = listeners(m.out("ss", "-H", "-tulnp"))
	if !m.ufwInstalled() {
		return f
	}
	f.Installed = true
	verbose := m.out("ufw", "status", "verbose")
	f.Active = strings.Contains(verbose, "Status: active")
	for _, l := range strings.Split(verbose, "\n") {
		if v, ok := strings.CutPrefix(l, "Default: "); ok {
			parts := strings.Split(v, ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if strings.HasSuffix(p, "(incoming)") {
					f.DefaultIn = strings.Fields(p)[0]
				}
				if strings.HasSuffix(p, "(outgoing)") {
					f.DefaultOut = strings.Fields(p)[0]
				}
			}
		}
	}
	f.Rules = parseRules(m.out("ufw", "status", "numbered"))
	return f
}

type RuleInput struct {
	Action  string `json:"action"` // allow | deny | reject | limit
	Port    string `json:"port"`   // 80 or 8000:8100
	Proto   string `json:"proto"`  // tcp | udp | any
	From    string `json:"from"`   // "" = anywhere
	Comment string `json:"comment"`
}

var portRe = regexp.MustCompile(`^(\d{1,5})(:\d{1,5})?$`)

func validPort(p string) bool {
	if !portRe.MatchString(p) {
		return false
	}
	for _, s := range strings.Split(p, ":") {
		if n := atoi(s); n < 1 || n > 65535 {
			return false
		}
	}
	return true
}

func cleanComment(c string) string {
	c = strings.Map(func(r rune) rune {
		if r < 32 || r == '\'' || r == '"' || r == '\\' || r == '`' || r == '$' {
			return -1
		}
		return r
	}, c)
	if len(c) > 60 {
		c = c[:60]
	}
	return strings.TrimSpace(c)
}

// AddRule appends a firewall rule.
func (m *Manager) AddRule(in RuleInput) error {
	if !m.ufwInstalled() {
		return invalid("UFW is not installed")
	}
	switch in.Action {
	case "allow", "deny", "reject", "limit":
	default:
		return invalid("action")
	}
	if !validPort(in.Port) {
		return invalid("port must be 1–65535 or a range like 8000:8100")
	}
	if strings.Contains(in.Port, ":") && in.Proto != "tcp" && in.Proto != "udp" {
		return invalid("a port range needs tcp or udp")
	}
	args := []string{in.Action}
	if in.From != "" {
		if _, _, err := net.ParseCIDR(in.From); err != nil && net.ParseIP(in.From) == nil {
			return invalid("source must be an IP address or network")
		}
		args = append(args, "from", in.From, "to", "any", "port", in.Port)
		switch in.Proto {
		case "tcp", "udp":
			args = append(args, "proto", in.Proto)
		case "", "any":
		default:
			return invalid("protocol")
		}
	} else {
		switch in.Proto {
		case "tcp", "udp":
			args = append(args, in.Port+"/"+in.Proto)
		case "", "any":
			args = append(args, in.Port)
		default:
			return invalid("protocol")
		}
	}
	if c := cleanComment(in.Comment); c != "" {
		args = append(args, "comment", c)
	}
	return m.Run.Run("ufw", args...)
}

// DeleteRule removes rule n, but only if it still reads as spec (rule numbers shift when others are removed).
func (m *Manager) DeleteRule(n int, spec string) error {
	var cur *FWRule
	for _, r := range parseRules(m.out("ufw", "status", "numbered")) {
		if r.N == n {
			r := r
			cur = &r
		}
	}
	if cur == nil || cur.To+"|"+cur.From != spec {
		return fmt.Errorf("%w: the rule list changed, reload and try again", ErrConflict)
	}
	return m.Run.Run("ufw", "--force", "delete", strconv.Itoa(n))
}

// EnableFirewall turns ufw on after making sure it cannot lock the Administrator out.
func (m *Manager) EnableFirewall() error {
	if !m.ufwInstalled() {
		return invalid("UFW is not installed")
	}
	have := m.out("ufw", "status")
	ssh := strconv.Itoa(m.sshPort())
	for _, p := range []struct{ port, why string }{{ssh + "/tcp", "SSH"}, {"80/tcp", "HTTP"}, {"443/tcp", "HTTPS"}} {
		if !strings.Contains(have, p.port) {
			if err := m.Run.Run("ufw", "allow", p.port, "comment", p.why); err != nil {
				return err
			}
		}
	}
	_ = m.Run.Run("ufw", "default", "deny", "incoming")
	_ = m.Run.Run("ufw", "default", "allow", "outgoing")
	return m.Run.Run("ufw", "--force", "enable")
}

func (m *Manager) DisableFirewall() error {
	if !m.ufwInstalled() {
		return invalid("UFW is not installed")
	}
	return m.Run.Run("ufw", "disable")
}

// --- SSH ---

type SSHSettings struct {
	Port          int    `json:"port"`
	RootLogin     string `json:"root_login"`    // yes | prohibit-password | no
	PasswordAuth  bool   `json:"password_auth"` // password logins for non-site users
	HasRootKey    bool   `json:"has_root_key"`
}

const sshDropIn = "/etc/ssh/sshd_config.d/00-hostingpanel.conf"

func (m *Manager) SSH() SSHSettings {
	s := SSHSettings{Port: 22, RootLogin: "prohibit-password", PasswordAuth: true}
	for _, l := range strings.Split(m.out("sshd", "-T"), "\n") {
		k, v, _ := strings.Cut(l, " ")
		switch k {
		case "port":
			s.Port = atoi(v)
		case "permitrootlogin":
			s.RootLogin = map[string]string{"without-password": "prohibit-password"}[v]
			if s.RootLogin == "" {
				s.RootLogin = v
			}
		case "passwordauthentication":
			s.PasswordAuth = v == "yes"
		}
	}
	s.HasRootKey = strings.TrimSpace(m.read("/root/.ssh/authorized_keys")) != ""
	return s
}

type SSHInput struct {
	RootLogin    string `json:"root_login"`
	PasswordAuth bool   `json:"password_auth"`
}

// SetSSH writes a drop-in, validates it with sshd -t and reloads. A bad file is removed again.
func (m *Manager) SetSSH(in SSHInput) error {
	switch in.RootLogin {
	case "yes", "prohibit-password", "no":
	default:
		return invalid("root login")
	}
	if !in.PasswordAuth && strings.TrimSpace(m.read("/root/.ssh/authorized_keys")) == "" && in.RootLogin != "no" {
		return fmt.Errorf("%w: root has no SSH key; turning passwords off would lock you out", ErrConflict)
	}
	prev := m.read(sshDropIn)
	pw := "yes"
	if !in.PasswordAuth {
		pw = "no"
	}
	if err := m.write(sshDropIn, fmt.Sprintf("# Managed by Hosting Panel\nPermitRootLogin %s\nPasswordAuthentication %s\n", in.RootLogin, pw), 0o644); err != nil {
		return err
	}
	if err := m.Run.Run("sshd", "-t"); err != nil {
		_ = m.write(sshDropIn, prev, 0o644)
		return fmt.Errorf("%w: sshd rejected the configuration: %v", ErrInvalid, err)
	}
	return m.Run.Run("systemctl", "reload", "ssh.service")
}
