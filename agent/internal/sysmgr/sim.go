package sysmgr

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Sim is a small simulated Ubuntu host for development and tests: it understands the commands the
// System Service issues and keeps just enough state (packages, units, firewall, bans, containers, FTP users)
// for the Panel to be exercised end to end without touching a real machine.
type Sim struct {
	mu   sync.Mutex
	Root string
	Log  []string

	pkgs      map[string]string // package -> version
	active    map[string]bool
	enabled   map[string]bool
	upgrades  []string
	ufwOn     bool
	ufwRules  []simRule
	bans      map[string][]string // jail -> ips
	totals    map[string]int
	ctrs      []Container
	imgs      []Image
	f2bLive   map[string]bool
	lastBoot  time.Time
	ctrSerial int
}

type simRule struct{ to, action, from, comment string }

func NewSim(root string) *Sim {
	s := &Sim{
		Root: root,
		pkgs: map[string]string{"nginx": "1.24.0-2ubuntu7", "openssh-server": "1:9.6p1-3ubuntu13", "rsync": "3.2.7-1build1"},
		active: map[string]bool{"nginx.service": true, "ssh.service": true, "cron.service": true, "hostingpanel-core.service": true,
			"hostingpanel-panel-fpm.service": true, "hostingpanel-system.service": true, "hostingpanel-mail.service": true,
			"hostingpanel-backup.service": true, "hostingpanel-dbmanager.service": true, "hostingpanel-notifier.service": true},
		bans:   map[string][]string{"sshd": {"185.224.128.17", "45.141.84.210"}},
		totals: map[string]int{"sshd": 14},
		f2bLive: map[string]bool{},
		upgrades: []string{
			"openssl/noble-updates,noble-security 3.0.13-0ubuntu3.5 amd64 [upgradable from: 3.0.13-0ubuntu3.4]",
			"libssl3t64/noble-updates,noble-security 3.0.13-0ubuntu3.5 amd64 [upgradable from: 3.0.13-0ubuntu3.4]",
			"curl/noble-updates,noble-security 8.5.0-2ubuntu10.6 amd64 [upgradable from: 8.5.0-2ubuntu10.5]",
			"nginx/noble-updates 1.24.0-2ubuntu7.1 amd64 [upgradable from: 1.24.0-2ubuntu7]",
			"tzdata/noble-updates 2025b-0ubuntu0.24.04 all [upgradable from: 2024b-0ubuntu0.24.04.1]",
		},
		lastBoot: time.Now().Add(-26 * time.Hour),
		imgs:     []Image{{ID: "3b1a5c2f9d10", Repo: "redis", Tag: "7-alpine", Size: "41.4MB", Created: "3 weeks ago"}},
	}
	s.enabled = map[string]bool{}
	for u := range s.active {
		s.enabled[u] = true
	}
	return s
}

func (s *Sim) record(name string, args ...string) {
	s.Log = append(s.Log, strings.TrimSpace(name+" "+strings.Join(args, " ")))
	if len(s.Log) > 500 {
		s.Log = s.Log[len(s.Log)-500:]
	}
}

func (s *Sim) Run(name string, args ...string) error {
	_, err := s.Output(name, args...)
	return err
}

func (s *Sim) RunW(w io.Writer, name string, args ...string) error {
	fmt.Fprintf(w, "$ %s %s\n", name, strings.Join(args, " "))
	out, err := s.Output(name, args...)
	if out != "" {
		fmt.Fprint(w, out)
	}
	if name == "apt-get" || name == "docker" {
		time.Sleep(700 * time.Millisecond) // long enough for the UI to show progress
	}
	return err
}

func (s *Sim) RunEnvW(w io.Writer, _ []string, _ string, name string, args ...string) error {
	return s.RunW(w, name, args...)
}

func (s *Sim) RunIn(stdin, name string, args ...string) error {
	if name == "pure-pw" {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.record(name, args...)
		return s.purePw(stdin, args)
	}
	return s.Run(name, args...)
}

func (s *Sim) path(p string) string { return filepath.Join(s.Root, p) }

// unitsFor maps packages to the units they bring in.
var simUnits = map[string][]string{
	"fail2ban": {"fail2ban.service"}, "ufw": {"ufw.service"}, "docker.io": {"docker.service"}, "redis-server": {"redis-server.service"},
	"memcached": {"memcached.service"}, "pure-ftpd": {"pure-ftpd.service"}, "supervisor": {"supervisor.service"}, "rabbitmq-server": {"rabbitmq-server.service"},
	"chrony": {"chrony.service"}, "vnstat": {"vnstat.service"}, "clamav-daemon": {"clamav-daemon.service"}, "unattended-upgrades": {"unattended-upgrades.service"},
}

func (s *Sim) Output(name string, args ...string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.record(name, args...)
	switch name {
	case "dpkg-query":
		var b strings.Builder
		for _, p := range args[3:] {
			if v, ok := s.pkgs[p]; ok {
				fmt.Fprintf(&b, "%s\tii \t%s\n", p, v)
			}
		}
		return b.String(), nil
	case "apt-get":
		return s.apt(args), nil
	case "apt":
		return "Listing...\n" + strings.Join(s.upgrades, "\n") + "\n", nil
	case "systemctl":
		return s.systemctl(args)
	case "fail2ban-client":
		return s.fail2ban(args)
	case "ufw":
		return s.ufw(args)
	case "ss":
		return simSS, nil
	case "sshd":
		return s.sshd(args), nil
	case "journalctl":
		return fmt.Sprintf("%s host %s[812]: started\n%s host %s[812]: ready to accept connections\n", time.Now().Add(-time.Hour).Format("2006-01-02T15:04:05-0700"), strings.TrimSuffix(args[1], ".service"), time.Now().Add(-59*time.Minute).Format("2006-01-02T15:04:05-0700"), strings.TrimSuffix(args[1], ".service")), nil
	case "docker":
		return s.docker(args)
	case "pure-pw":
		return "", s.purePw("", args)
	}
	return "", nil
}

func (s *Sim) apt(args []string) string {
	var verb string
	var pk []string
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if a == "-o" {
			skip = true
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		if verb == "" {
			verb = a
			continue
		}
		pk = append(pk, a)
	}
	switch verb {
	case "install":
		for _, p := range pk {
			s.pkgs[p] = "1.0-simulated"
			for _, u := range simUnits[p] {
				s.active[u], s.enabled[u] = true, true
			}
		}
		return fmt.Sprintf("Reading package lists...\nSetting up %s ...\n", strings.Join(pk, " "))
	case "remove":
		for _, p := range pk {
			delete(s.pkgs, p)
			for _, u := range simUnits[p] {
				delete(s.active, u)
				delete(s.enabled, u)
			}
		}
		return "Removing packages...\n"
	case "upgrade":
		s.upgrades = nil
		return "0 upgraded.\n"
	}
	return "Hit:1 http://archive.ubuntu.com/ubuntu noble InRelease\nReading package lists...\n"
}

func (s *Sim) systemctl(a []string) (string, error) {
	if len(a) == 0 {
		return "", nil
	}
	switch a[0] {
	case "is-active":
		var b strings.Builder
		for _, u := range a[1:] {
			if s.active[u] {
				b.WriteString("active\n")
			} else {
				b.WriteString("inactive\n")
			}
		}
		return b.String(), nil
	case "is-enabled":
		var b strings.Builder
		for _, u := range a[1:] {
			if s.enabled[u] {
				b.WriteString("enabled\n")
			} else {
				b.WriteString("disabled\n")
			}
		}
		return b.String(), nil
	case "list-units":
		var us []string
		for u := range s.active {
			us = append(us, u)
		}
		for u := range s.enabled {
			if !s.active[u] {
				us = append(us, u)
			}
		}
		sort.Strings(us)
		var b strings.Builder
		seen := map[string]bool{}
		for _, u := range us {
			if seen[u] {
				continue
			}
			seen[u] = true
			st, sub := "inactive", "dead"
			if s.active[u] {
				st, sub = "active", "running"
			}
			fmt.Fprintf(&b, "%s loaded %s %s %s\n", u, st, sub, strings.TrimSuffix(u, ".service")+" service")
		}
		fmt.Fprintf(&b, "php8.3-fpm.service loaded active running PHP 8.3 FastCGI Process Manager\n")
		return b.String(), nil
	case "list-unit-files":
		var b strings.Builder
		for u := range s.enabled {
			fmt.Fprintf(&b, "%s enabled enabled\n", u)
		}
		return b.String(), nil
	case "start", "restart", "reload-or-restart", "reload":
		s.active[a[1]] = true
		if a[0] == "reload-or-restart" && a[1] == "fail2ban.service" {
			s.reloadF2B()
		}
	case "stop":
		s.active[a[1]] = false
	case "enable":
		s.enabled[a[1]] = true
	case "disable":
		s.enabled[a[1]] = false
	case "reboot":
		s.lastBoot = time.Now()
	}
	return "", nil
}

// reloadF2B re-reads the jail files the Manager wrote.
func (s *Sim) reloadF2B() {
	s.f2bLive = map[string]bool{}
	files, _ := filepath.Glob(s.path("/etc/fail2ban/jail.d/hp-*.local"))
	for _, f := range files {
		b, _ := os.ReadFile(f)
		sec := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "hp-"), ".local")
		if ini(string(b))["enabled"] == "true" {
			s.f2bLive[sec] = true
		}
	}
}

func (s *Sim) fail2ban(a []string) (string, error) {
	if !s.active["fail2ban.service"] {
		return "", fmt.Errorf("fail2ban-client: not running")
	}
	if len(s.f2bLive) == 0 && len(a) > 0 && a[0] == "status" {
		s.reloadF2B()
	}
	if len(a) == 1 && a[0] == "status" {
		var js []string
		for j := range s.f2bLive {
			js = append(js, j)
		}
		sort.Strings(js)
		return fmt.Sprintf("Status\n|- Number of jail:\t%d\n`- Jail list:\t%s\n", len(js), strings.Join(js, ", ")), nil
	}
	switch a[0] {
	case "status":
		j := a[1]
		ips := s.bans[j]
		return fmt.Sprintf("Status for the jail: %s\n|- Filter\n|  |- Currently failed:\t%d\n|  |- Total failed:\t%d\n|  `- File list:\t/var/log/auth.log\n`- Actions\n   |- Currently banned:\t%d\n   |- Total banned:\t%d\n   `- Banned IP list:\t%s\n",
			j, len(ips)+2, s.totals[j]+30, len(ips), s.totals[j], strings.Join(ips, " ")), nil
	case "get":
		d := ini(func() string { b, _ := os.ReadFile(s.path("/etc/fail2ban/jail.d/hp-" + a[1] + ".local")); return string(b) }())
		v := d[a[2]]
		if v == "" {
			v = map[string]string{"bantime": "3600", "findtime": "600", "maxretry": "5"}[a[2]]
		}
		return v + "\n", nil
	case "set":
		j, op, ip := a[1], a[2], a[3]
		if op == "banip" {
			s.bans[j] = append(s.bans[j], ip)
			s.totals[j]++
		} else if op == "unbanip" {
			var keep []string
			for _, x := range s.bans[j] {
				if x != ip {
					keep = append(keep, x)
				}
			}
			s.bans[j] = keep
		}
	}
	return "", nil
}

func (s *Sim) ufw(a []string) (string, error) {
	if !s.pkgsHas("ufw") {
		return "", fmt.Errorf("ufw: not installed")
	}
	if len(a) > 0 && a[0] == "--force" {
		a = a[1:]
		if len(a) > 0 && a[0] == "delete" {
			n := atoi(a[1])
			if n >= 1 && n <= len(s.ufwRules) {
				s.ufwRules = append(s.ufwRules[:n-1], s.ufwRules[n:]...)
			}
			return "", nil
		}
		if len(a) > 0 && a[0] == "enable" {
			s.ufwOn = true
		}
		return "", nil
	}
	switch a[0] {
	case "status":
		if len(a) > 1 && a[1] == "numbered" || len(a) > 1 && a[1] == "verbose" || len(a) == 1 {
			var b strings.Builder
			if !s.ufwOn {
				return "Status: inactive\n", nil
			}
			b.WriteString("Status: active\n")
			if len(a) > 1 && a[1] == "verbose" {
				b.WriteString("Logging: on (low)\nDefault: deny (incoming), allow (outgoing), disabled (routed)\nNew profiles: skip\n")
			}
			b.WriteString("\n     To                         Action      From\n     --                         ------      ----\n")
			for i, r := range s.ufwRules {
				c := ""
				if r.comment != "" {
					c = "   # " + r.comment
				}
				fmt.Fprintf(&b, "[%2d] %-26s %-11s %s%s\n", i+1, r.to, r.action+" IN", r.from, c)
			}
			return b.String(), nil
		}
	case "disable":
		s.ufwOn = false
	case "allow", "deny", "reject", "limit":
		r := simRule{action: strings.ToUpper(a[0]), from: "Anywhere"}
		rest := a[1:]
		if i := indexOf(rest, "comment"); i >= 0 {
			r.comment = strings.Join(rest[i+1:], " ")
			rest = rest[:i]
		}
		if len(rest) > 0 && rest[0] == "from" {
			r.from = rest[1]
			r.to = rest[5]
			if i := indexOf(rest, "proto"); i >= 0 {
				r.to += "/" + rest[i+1]
			}
		} else {
			r.to = rest[0]
		}
		s.ufwRules = append(s.ufwRules, r)
	}
	return "", nil
}

func indexOf(a []string, x string) int {
	for i, v := range a {
		if v == x {
			return i
		}
	}
	return -1
}

func (s *Sim) pkgsHas(p string) bool { _, ok := s.pkgs[p]; return ok }

func (s *Sim) sshd(a []string) string {
	if len(a) > 0 && a[0] == "-T" {
		root, pw := "without-password", "yes"
		b, _ := os.ReadFile(s.path(sshDropIn))
		for _, l := range strings.Split(string(b), "\n") {
			f := strings.Fields(l)
			if len(f) == 2 && f[0] == "PermitRootLogin" {
				root = map[string]string{"prohibit-password": "without-password"}[f[1]]
				if root == "" {
					root = f[1]
				}
			}
			if len(f) == 2 && f[0] == "PasswordAuthentication" {
				pw = f[1]
			}
		}
		return fmt.Sprintf("port 22\npermitrootlogin %s\npasswordauthentication %s\n", root, pw)
	}
	return ""
}

const simSS = `udp   UNCONN 0 0 127.0.0.53%lo:53 0.0.0.0:* users:(("systemd-resolve",pid=612,fd=13))
tcp   LISTEN 0 4096 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=801,fd=3))
tcp   LISTEN 0 511 0.0.0.0:80 0.0.0.0:* users:(("nginx",pid=905,fd=6))
tcp   LISTEN 0 511 0.0.0.0:443 0.0.0.0:* users:(("nginx",pid=905,fd=7))
tcp   LISTEN 0 80 127.0.0.1:3306 0.0.0.0:* users:(("mariadbd",pid=1022,fd=21))
tcp   LISTEN 0 100 0.0.0.0:25 0.0.0.0:* users:(("master",pid=1180,fd=13))
tcp   LISTEN 0 4096 [::]:22 [::]:* users:(("sshd",pid=801,fd=4))
`

func (s *Sim) docker(a []string) (string, error) {
	if len(a) == 0 {
		return "", nil
	}
	switch a[0] {
	case "ps":
		var b strings.Builder
		for _, c := range s.ctrs {
			fmt.Fprintf(&b, `{"ID":"%s","Names":"%s","Image":"%s","State":"%s","Status":"%s","Ports":"%s"}`+"\n", c.ID, c.Name, c.Image, c.State, c.Status, c.Ports)
		}
		return b.String(), nil
	case "images":
		var b strings.Builder
		for _, i := range s.imgs {
			fmt.Fprintf(&b, `{"ID":"%s","Repository":"%s","Tag":"%s","Size":"%s","CreatedSince":"%s"}`+"\n", i.ID, i.Repo, i.Tag, i.Size, i.Created)
		}
		return b.String(), nil
	case "run":
		img := a[len(a)-1]
		name, ports := "", ""
		for i, x := range a {
			if x == "--name" {
				name = a[i+1]
			}
			if x == "-p" {
				ports += a[i+1] + " "
			}
		}
		s.ctrSerial++
		id := fmt.Sprintf("%012x", 0xa1b2c3d4e5f0+s.ctrSerial*7919)
		if name == "" {
			name = fmt.Sprintf("sim_container_%d", s.ctrSerial)
		}
		s.ctrs = append(s.ctrs, Container{ID: id, Name: name, Image: img, State: "running", Status: "Up 2 seconds", Ports: strings.TrimSpace(ports)})
		return id + "\n", nil
	case "start", "restart", "unpause":
		s.setState(a[1], "running", "Up 1 second")
	case "stop", "kill":
		s.setState(a[1], "exited", "Exited (0) 1 second ago")
	case "pause":
		s.setState(a[1], "paused", "Up (Paused)")
	case "rm":
		var keep []Container
		for _, c := range s.ctrs {
			if c.ID != a[len(a)-1] {
				keep = append(keep, c)
			}
		}
		s.ctrs = keep
	case "pull":
		repo, tag, _ := strings.Cut(a[1], ":")
		if tag == "" {
			tag = "latest"
		}
		s.imgs = append(s.imgs, Image{ID: fmt.Sprintf("%012x", 0xf00d00+len(s.imgs)*31), Repo: repo, Tag: tag, Size: "128MB", Created: "2 days ago"})
		return "Status: Downloaded newer image for " + a[1] + "\n", nil
	case "rmi":
		var keep []Image
		for _, i := range s.imgs {
			if i.ID != a[1] {
				keep = append(keep, i)
			}
		}
		s.imgs = keep
	case "logs":
		return "2025-01-01T10:00:00Z Server started\n2025-01-01T10:00:01Z Listening on :80\n", nil
	}
	return "", nil
}

func (s *Sim) setState(id, state, status string) {
	for i := range s.ctrs {
		if s.ctrs[i].ID == id {
			s.ctrs[i].State, s.ctrs[i].Status = state, status
		}
	}
}

func (s *Sim) purePw(stdin string, a []string) error {
	p := s.path(ftpPasswd)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	b, _ := os.ReadFile(p)
	lines := []string{}
	for _, l := range strings.Split(string(b), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	switch a[0] {
	case "useradd":
		uid, home := "1001", ""
		for i, x := range a {
			if x == "-u" {
				uid = a[i+1]
			}
			if x == "-d" {
				home = a[i+1]
			}
		}
		lines = append(lines, fmt.Sprintf("%s:%s:%s:%s::%s/./::::::::::::", a[1], "$sim$"+strings.TrimSpace(stdin)[:3], uid, uid, home))
	case "userdel":
		keep := lines[:0]
		for _, l := range lines {
			if !strings.HasPrefix(l, a[1]+":") {
				keep = append(keep, l)
			}
		}
		lines = keep
	}
	return os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}
