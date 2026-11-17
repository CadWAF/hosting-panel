package sysmgr

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/tasks"
)

// CadWAF Firewall is an optional eBPF/XDP host firewall (default-deny, per-port ACLs, allow/block lists).
// It is installed from a bundle shipped with the Panel and managed through its local REST API.
const (
	cwDir       = "/etc/firewall"
	cwEnv       = cwDir + "/firewalld.env"
	cwBin       = "/usr/local/bin/firewalld"
	cwCtl       = "/usr/local/bin/firewallctl"
	cwUnit      = "/etc/systemd/system/firewalld.service"
	cwVersion   = cwDir + "/.hp-version"
	cwBundleDir = "/usr/share/hostingpanel/firewall"
	maxPorts    = 4096
)

type CWPorts struct {
	TCPIn          []int `json:"tcp_in"`
	UDPIn          []int `json:"udp_in"`
	TCPOut         []int `json:"tcp_out"`
	UDPOut         []int `json:"udp_out"`
	AllowLocalhost bool  `json:"allow_localhost"`
}

type CWRule struct {
	ID     int    `json:"id"`
	Rule   string `json:"rule"`
	Action string `json:"action"`
	File   string `json:"file"`
}

type CWDrop struct {
	IP       string `json:"ip"`
	Reason   string `json:"reason"`
	Proto    string `json:"proto"`
	Packets  uint64 `json:"packets"`
	LastPort int    `json:"last_dst_port"`
	LastSeen string `json:"last_seen"`
}

type CWStats struct {
	XDPPass uint64 `json:"xdp_pass"`
	XDPDrop uint64 `json:"xdp_drop"`
	TCPass  uint64 `json:"tc_pass"`
	TCDrop  uint64 `json:"tc_drop"`
}

type CadWAF struct {
	Bundled   bool     `json:"bundled"`   // the installer files are present on this server
	Installed bool     `json:"installed"` // binaries and unit are in place
	Active    bool     `json:"active"`    // systemd unit is running
	Attached  bool     `json:"attached"`  // XDP program is attached to the interface
	Status    string   `json:"status"`    // ok | degraded | down
	Version   string   `json:"version"`
	Iface     string   `json:"iface"`
	Egress    bool     `json:"egress"` // outbound (TC) filtering
	Ports     CWPorts  `json:"ports"`
	Whitelist []string `json:"whitelist"`
	Blocklist []string `json:"blocklist"`
	Rules     []CWRule `json:"rules"`
	Stats     CWStats  `json:"stats"`
	Drops     []CWDrop `json:"drops"`
	UFWActive bool     `json:"ufw_active"` // another firewall is on: both would filter
	SSHPort   int      `json:"ssh_port"`
	Error     string   `json:"error,omitempty"`
}

func (m *Manager) cwBundle() string {
	arch := runtime.GOARCH
	if m.Root != "" { // development host: any architecture counts
		return m.path(cwBundleDir)
	}
	return filepath.Join(cwBundleDir, arch)
}

func (m *Manager) cwBundled() bool {
	if m.Root != "" {
		return true // the simulated host installs a stand-in
	}
	return exists(filepath.Join(m.cwBundle(), "firewalld")) && exists(filepath.Join(m.cwBundle(), "firewallctl"))
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func (m *Manager) cwInstalled() bool { return m.exists(cwBin) && m.exists(cwUnit) }

func envValue(env, key string) string {
	for _, l := range strings.Split(env, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(l), key+"="); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

// setEnv rewrites (or appends) KEY=value in an env file body.
func setEnv(env, key, value string) string {
	lines := strings.Split(strings.TrimRight(env, "\n"), "\n")
	done := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), key+"=") {
			lines[i] = key + "=" + value
			done = true
		}
	}
	if !done {
		lines = append(lines, key+"="+value)
	}
	return strings.Join(lines, "\n") + "\n"
}

// cwCall talks to the firewall daemon's local REST API (replaceable in tests and the development simulator).
func (m *Manager) cwCall(method, path string, body any) (map[string]any, error) {
	if m.CW != nil {
		return m.CW(method, path, body)
	}
	env := m.read(cwEnv)
	addr := envValue(env, "FIREWALL_LISTEN_ADDR")
	if addr == "" {
		addr = "127.0.0.1:9090"
	}
	if host, _, err := net.SplitHostPort(addr); err != nil || (host != "127.0.0.1" && host != "localhost" && host != "::1" && host != "") {
		return nil, fmt.Errorf("%w: the firewall API must listen on loopback, found %q", ErrInvalid, addr)
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://"+addr+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+envValue(env, "FIREWALL_API_TOKEN"))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("the firewall service is not answering: %w", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	if resp.StatusCode >= 400 {
		msg, _ := out["error"].(string)
		if msg == "" {
			msg = strings.TrimSpace(string(b))
		}
		if resp.StatusCode < 500 {
			return nil, invalid("%s", msg)
		}
		return nil, fmt.Errorf("firewall: %s", msg)
	}
	return out, nil
}

func ints(v any) []int {
	out := []int{}
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if f, ok := x.(float64); ok {
				out = append(out, int(f))
			}
		}
	}
	return out
}

func strs(v any) []string {
	out := []string{}
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func num(v any) uint64 { f, _ := v.(float64); return uint64(f) }

// Firewall2 reports the CadWAF Firewall: installer availability, service state and the live policy.
func (m *Manager) CadWAF() CadWAF {
	c := CadWAF{Bundled: m.cwBundled(), Installed: m.cwInstalled(), Status: "down", Ports: CWPorts{TCPIn: []int{}, UDPIn: []int{}, TCPOut: []int{}, UDPOut: []int{}},
		Whitelist: []string{}, Blocklist: []string{}, Rules: []CWRule{}, Drops: []CWDrop{}, SSHPort: m.sshPort()}
	c.UFWActive = m.ufwInstalled() && strings.Contains(m.out("ufw", "status"), "Status: active")
	if !c.Installed {
		return c
	}
	c.Version = strings.TrimSpace(m.read(cwVersion))
	c.Egress = !strings.EqualFold(envValue(m.read(cwEnv), "FIREWALL_EGRESS"), "false")
	us := m.unitStates([]string{"firewalld.service"})["firewalld.service"]
	c.Active = us.Active
	if !c.Active {
		return c
	}
	h, err := m.cwCall("GET", "/health", nil)
	if err != nil {
		c.Error = err.Error()
		return c
	}
	c.Iface, _ = h["iface"].(string)
	c.Attached, _ = h["attached"].(bool)
	c.Status, _ = h["status"].(string)
	if p, err := m.cwCall("GET", "/api/v1/ports", nil); err == nil {
		c.Ports = CWPorts{TCPIn: ints(p["tcp_in"]), UDPIn: ints(p["udp_in"]), TCPOut: ints(p["tcp_out"]), UDPOut: ints(p["udp_out"])}
		c.Ports.AllowLocalhost, _ = p["allow_localhost"].(bool)
	}
	if r, err := m.cwCall("GET", "/api/v1/ips/whitelist", nil); err == nil {
		c.Whitelist = strs(r["ips"])
	}
	if r, err := m.cwCall("GET", "/api/v1/ips/blocklist", nil); err == nil {
		c.Blocklist = strs(r["ips"])
	}
	if r, err := m.cwCall("GET", "/api/v1/rules", nil); err == nil {
		if l, ok := r["rules"].([]any); ok {
			for _, x := range l {
				if o, ok := x.(map[string]any); ok {
					rule, _ := o["rule"].(string)
					act := fmt.Sprint(o["action"])
					file, _ := o["file"].(string)
					c.Rules = append(c.Rules, CWRule{ID: int(num(o["id"])), Rule: rule, Action: act, File: file})
				}
			}
		}
	}
	if s, err := m.cwCall("GET", "/api/v1/stats", nil); err == nil {
		x, _ := s["xdp"].(map[string]any)
		t, _ := s["tc"].(map[string]any)
		c.Stats = CWStats{XDPPass: num(x["pass"]), XDPDrop: num(x["drop"]), TCPass: num(t["pass"]), TCDrop: num(t["drop"])}
	}
	if d, err := m.cwCall("GET", "/api/v1/drops?minutes=1440&limit=30", nil); err == nil {
		if l, ok := d["drops"].([]any); ok {
			for _, x := range l {
				if o, ok := x.(map[string]any); ok {
					ip, _ := o["ip"].(string)
					rs, _ := o["reason"].(string)
					pr, _ := o["proto"].(string)
					ls, _ := o["last_seen"].(string)
					c.Drops = append(c.Drops, CWDrop{IP: ip, Reason: rs, Proto: pr, Packets: num(o["packets"]), LastPort: int(num(o["last_dst_port"])), LastSeen: ls})
				}
			}
			sort.Slice(c.Drops, func(i, j int) bool { return c.Drops[i].Packets > c.Drops[j].Packets })
		}
	}
	return c
}

// ---- install / remove ----

func validIPOrNet(s string) bool {
	if _, _, err := net.ParseCIDR(s); err == nil {
		return true
	}
	return net.ParseIP(s) != nil
}

// publicPorts lists the TCP and UDP ports something already listens on for the outside world, so a fresh
// default-deny policy does not cut off services that are running today.
func (m *Manager) publicPorts() (tcp, udp []int) {
	seenT, seenU := map[int]bool{}, map[int]bool{}
	for _, l := range listeners(m.out("ss", "-H", "-tulnp")) {
		if !l.Public || l.Port < 1 || l.Port > 65535 {
			continue
		}
		if strings.HasPrefix(l.Proto, "tcp") && !seenT[l.Port] {
			seenT[l.Port] = true
			tcp = append(tcp, l.Port)
		}
		if strings.HasPrefix(l.Proto, "udp") && !seenU[l.Port] {
			seenU[l.Port] = true
			udp = append(udp, l.Port)
		}
	}
	return
}

func addPorts(l []int, extra ...int) []int {
	seen := map[int]bool{}
	for _, p := range l {
		seen[p] = true
	}
	for _, p := range extra {
		if p > 0 && !seen[p] {
			seen[p] = true
			l = append(l, p)
		}
	}
	sort.Ints(l)
	return l
}

func joinPorts(l []int) string {
	s := make([]string, len(l))
	for i, p := range l {
		s[i] = strconv.Itoa(p)
	}
	return strings.Join(s, ",")
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

const cwUnitFile = `[Unit]
Description=CadWAF eBPF/XDP Host Firewall
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/firewalld
ExecReload=/bin/kill -HUP $MAINPID
ExecStopPost=-/usr/local/bin/firewallctl hooks-off
EnvironmentFile=-/etc/firewall/firewalld.env
Restart=on-failure
RestartSec=5
LimitMEMLOCK=infinity

[Install]
WantedBy=multi-user.target
`

// InstallCadWAF installs the firewall as a Task. allowIP (the Administrator's current address) is placed on the
// allow list so the first start cannot lock the Panel's user out; open SSH/web ports and everything already
// listening publicly are allowed. Outbound filtering starts off.
func (m *Manager) InstallCadWAF(allowIP string) (store.Task, error) {
	if m.cwInstalled() {
		return store.Task{}, fmt.Errorf("%w: already installed", ErrConflict)
	}
	if !m.cwBundled() {
		return store.Task{}, invalid("the firewall installer is not on this server; reinstall the Panel from a build that includes it")
	}
	allowIP = strings.TrimSpace(allowIP)
	if allowIP != "" && !validIPOrNet(allowIP) {
		return store.Task{}, invalid("allowed address")
	}
	return m.Tasks.Start("cadwaf.install", "Install CadWAF Firewall", func(l *tasks.Log) error {
		m.apt.Lock()
		defer m.apt.Unlock()
		if m.Root == "" {
			if rel := strings.TrimSpace(m.out("uname", "-r")); rel != "" {
				l.Printf("Kernel %s", rel)
				var maj, min int
				if _, err := fmt.Sscanf(rel, "%d.%d", &maj, &min); err == nil && (maj < 5 || (maj == 5 && min < 10)) {
					return fmt.Errorf("kernel 5.10 or newer is required")
				}
			}
		}
		if m.Run.Run("sh", "-c", "command -v ip && command -v tc") != nil {
			l.Printf("Installing iproute2")
			if err := m.Run.RunW(l, "apt-get", "-y", "-q", "install", "iproute2"); err != nil {
				return err
			}
		}
		l.Printf("Copying binaries")
		if m.Root == "" {
			for _, b := range []string{"firewalld", "firewallctl"} {
				if err := m.Run.RunW(l, "install", "-m", "0755", filepath.Join(m.cwBundle(), b), "/usr/local/bin/"+b); err != nil {
					return err
				}
			}
		} else {
			_ = m.write(cwBin, "#!/bin/sh\n", 0o755)
			_ = m.write(cwCtl, "#!/bin/sh\n", 0o755)
		}
		ssh := m.sshPort()
		tcp, udp := m.publicPorts()
		tcpIn := addPorts(tcp, ssh, 80, 443)
		if m.pkgInstalled("pure-ftpd") {
			tcpIn = addPorts(tcpIn, 21)
		}
		udpIn := addPorts(udp)
		ports := fmt.Sprintf("# managed by Hosting Panel (editable in the panel or by hand)\ntcp_in = %s\nudp_in = %s\ntcp_out = 22,25,53,80,443,465,587,993,995,123\nudp_out = 53,123\nallow_localhost = true\n", joinPorts(tcpIn), joinPorts(udpIn))
		if len(udpIn) == 0 {
			ports = strings.Replace(ports, "udp_in = \n", "udp_in = 443\n", 1)
		}
		allow := "# Whitelisted IPs bypass all firewall rules.\n127.0.0.1\n::1\n"
		if allowIP != "" {
			allow += allowIP + "\n"
		}
		if !m.exists(cwDir + "/ports.conf") {
			if err := m.write(cwDir+"/ports.conf", ports, 0o644); err != nil {
				return err
			}
		}
		if !m.exists(cwDir + "/firewall.allow") {
			if err := m.write(cwDir+"/firewall.allow", allow, 0o644); err != nil {
				return err
			}
		}
		if !m.exists(cwDir + "/firewall.deny") {
			if err := m.write(cwDir+"/firewall.deny", "# Blocked IPs, one per line.\n", 0o644); err != nil {
				return err
			}
		}
		if !m.exists(cwEnv) {
			env := "FIREWALL_API_TOKEN=" + randomToken() + "\nFIREWALL_CONFIG_DIR=/etc/firewall\nFIREWALL_LISTEN_ADDR=127.0.0.1:9090\nFIREWALL_EGRESS=false\nFIREWALL_DIAG_LOG=/var/log/firewalld-diag.log\nFIREWALL_DIAG_INTERVAL=15m\nFIREWALL_MAP_REFRESH_INTERVAL=12h\nFIREWALL_DROP_LOG=/var/lib/firewall/drops.sqlite\nFIREWALL_DROP_RETENTION=336h\n"
			if err := m.write(cwEnv, env, 0o600); err != nil {
				return err
			}
		}
		if err := m.write(cwUnit, cwUnitFile, 0o644); err != nil {
			return err
		}
		_ = m.write(cwVersion, "0.3.0\n", 0o644)
		l.Printf("Inbound ports allowed at first start: TCP %s", joinPorts(tcpIn))
		if allowIP != "" {
			l.Printf("Allow-listed your address %s", allowIP)
		}
		if err := m.Run.RunW(l, "systemctl", "daemon-reload"); err != nil {
			return err
		}
		l.Printf("Starting the firewall")
		if err := m.Run.RunW(l, "systemctl", "enable", "--now", "firewalld.service"); err != nil {
			_ = m.Run.Run("systemctl", "stop", "firewalld.service")
			return err
		}
		deadline := time.Now().Add(25 * time.Second)
		var last error
		for time.Now().Before(deadline) {
			h, err := m.cwCall("GET", "/health", nil)
			if err == nil {
				if at, _ := h["attached"].(bool); at {
					l.Printf("Firewall is attached to %v", h["iface"])
					return nil
				}
				last = fmt.Errorf("the firewall started but is not attached to a network interface")
			} else {
				last = err
			}
			time.Sleep(time.Second)
			if m.Root != "" {
				break
			}
		}
		if last != nil && m.Root == "" {
			_ = m.Run.Run("systemctl", "stop", "firewalld.service") // never leave a half-working filter behind
			return fmt.Errorf("%v; the service was stopped so nothing is filtered", last)
		}
		return nil
	})
}

func (m *Manager) pkgInstalled(p string) bool { return len(m.dpkgState([]string{p})) > 0 }

// RemoveCadWAF stops the firewall, detaches its eBPF programs and removes the program (configuration is kept).
func (m *Manager) RemoveCadWAF() (store.Task, error) {
	if !m.cwInstalled() {
		return store.Task{}, ErrNotFound
	}
	return m.Tasks.Start("cadwaf.remove", "Remove CadWAF Firewall", func(l *tasks.Log) error {
		_ = m.Run.RunW(l, "systemctl", "disable", "--now", "firewalld.service")
		_ = m.Run.RunW(l, cwCtl, "hooks-off")
		for _, p := range []string{cwUnit, cwBin, cwCtl, cwVersion} {
			_ = os.Remove(m.path(p))
		}
		l.Printf("Removed. Configuration in %s was kept.", cwDir)
		return m.Run.RunW(l, "systemctl", "daemon-reload")
	})
}

// ---- management ----

func (m *Manager) cwReady() error {
	if !m.cwInstalled() {
		return invalid("CadWAF Firewall is not installed")
	}
	return nil
}

func expandPorts(l []int) ([]int, error) {
	seen := map[int]bool{}
	out := []int{}
	for _, p := range l {
		if p < 1 || p > 65535 {
			return nil, invalid("port %d is out of range", p)
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	if len(out) > maxPorts {
		return nil, invalid("at most %d ports per list", maxPorts)
	}
	sort.Ints(out)
	return out, nil
}

// SetCWPorts replaces the port lists. The daemon refuses a policy that would cut off SSH.
func (m *Manager) SetCWPorts(p CWPorts) error {
	if err := m.cwReady(); err != nil {
		return err
	}
	body := map[string]any{"allow_localhost": p.AllowLocalhost}
	for k, l := range map[string][]int{"tcp_in": p.TCPIn, "udp_in": p.UDPIn, "tcp_out": p.TCPOut, "udp_out": p.UDPOut} {
		x, err := expandPorts(l)
		if err != nil {
			return err
		}
		body[k] = x
	}
	_, err := m.cwCall("PUT", "/api/v1/ports", body)
	return err
}

func cwList(list string) (string, error) {
	switch list {
	case "allow":
		return "whitelist", nil
	case "block":
		return "blocklist", nil
	}
	return "", invalid("list")
}

func (m *Manager) AddCWIP(list, ip string) error {
	if err := m.cwReady(); err != nil {
		return err
	}
	p, err := cwList(list)
	if err != nil {
		return err
	}
	ip = strings.TrimSpace(ip)
	if !validIPOrNet(ip) {
		return invalid("IP address or network")
	}
	_, err = m.cwCall("POST", "/api/v1/ips/"+p, map[string]string{"ip": ip})
	return err
}

func (m *Manager) DeleteCWIP(list, ip string) error {
	if err := m.cwReady(); err != nil {
		return err
	}
	p, err := cwList(list)
	if err != nil {
		return err
	}
	if !validIPOrNet(ip) {
		return invalid("IP address or network")
	}
	_, err = m.cwCall("DELETE", "/api/v1/ips/"+p+"/"+strings.ReplaceAll(ip, "/", "%2F"), nil)
	return err
}

func (m *Manager) AddCWRule(rule, file string) error {
	if err := m.cwReady(); err != nil {
		return err
	}
	if file != "allow" && file != "deny" {
		return invalid("file")
	}
	rule = strings.TrimSpace(rule)
	if rule == "" || len(rule) > 200 || strings.ContainsAny(rule, "\n\r") {
		return invalid("rule")
	}
	_, err := m.cwCall("POST", "/api/v1/rules", map[string]string{"rule": rule, "file": file})
	return err
}

func (m *Manager) DeleteCWRule(id int, file string) error {
	if err := m.cwReady(); err != nil {
		return err
	}
	if file != "allow" && file != "deny" {
		return invalid("file")
	}
	_, err := m.cwCall("DELETE", "/api/v1/rules/"+strconv.Itoa(id)+"?file="+file, nil)
	return err
}

func (m *Manager) ReloadCW() error {
	if err := m.cwReady(); err != nil {
		return err
	}
	_, err := m.cwCall("POST", "/api/v1/reload", nil)
	return err
}

// SetCWEgress turns outbound (TC) filtering on or off; the service restarts to re-attach.
func (m *Manager) SetCWEgress(on bool) error {
	if err := m.cwReady(); err != nil {
		return err
	}
	v := "true"
	if !on {
		v = "false"
	}
	if err := m.write(cwEnv, setEnv(m.read(cwEnv), "FIREWALL_EGRESS", v), 0o600); err != nil {
		return err
	}
	return m.Run.Run("systemctl", "restart", "firewalld.service")
}

// CWService starts, stops or restarts the firewall. "detach" is the emergency switch: it stops the service and
// removes the eBPF programs, leaving the server unfiltered by this firewall.
func (m *Manager) CWService(action string) error {
	if err := m.cwReady(); err != nil {
		return err
	}
	switch action {
	case "start", "stop", "restart":
		return m.Run.Run("systemctl", action, "firewalld.service")
	case "detach":
		_ = m.Run.Run("systemctl", "stop", "firewalld.service")
		return m.Run.Run(cwCtl, "hooks-off")
	}
	return invalid("action")
}
