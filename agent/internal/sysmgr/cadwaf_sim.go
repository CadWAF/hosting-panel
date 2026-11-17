package sysmgr

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// NewCWFake returns an in-memory stand-in for the CadWAF Firewall daemon's REST API (development dry-run and tests).
// It reads the port lists the Manager wrote so the simulated policy matches the files.
func NewCWFake(m *Manager) func(method, path string, body any) (map[string]any, error) {
	var mu sync.Mutex
	wl, bl := []string{"127.0.0.1", "::1"}, []string{}
	type rule struct{ rule, file string }
	rules := []rule{}
	var ports *CWPorts
	var xdpPass, xdpDrop uint64 = 48210, 912
	started := time.Now()
	load := func() {
		if ports != nil {
			return
		}
		ports = &CWPorts{TCPIn: []int{}, UDPIn: []int{}, TCPOut: []int{}, UDPOut: []int{}, AllowLocalhost: true}
		for _, l := range strings.Split(m.read(cwDir+"/ports.conf"), "\n") {
			k, v, ok := strings.Cut(l, "=")
			if !ok {
				continue
			}
			var dst *[]int
			switch strings.TrimSpace(k) {
			case "tcp_in":
				dst = &ports.TCPIn
			case "udp_in":
				dst = &ports.UDPIn
			case "tcp_out":
				dst = &ports.TCPOut
			case "udp_out":
				dst = &ports.UDPOut
			}
			if dst == nil {
				continue
			}
			for _, p := range strings.Split(v, ",") {
				if n := atoi(p); n > 0 {
					*dst = append(*dst, n)
				}
			}
		}
		for _, l := range strings.Split(m.read(cwDir+"/firewall.allow"), "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") && !contains(wl, l) {
				wl = append(wl, l)
			}
		}
	}
	handle := func(method, path string, body any) (map[string]any, error) {
		mu.Lock()
		defer mu.Unlock()
		load()
		in := map[string]any{}
		if body != nil {
			b, _ := json.Marshal(body)
			_ = json.Unmarshal(b, &in)
		}
		p, _, _ := strings.Cut(path, "?")
		switch {
		case method == "GET" && p == "/health":
			xdpPass += uint64(time.Since(started).Seconds()) * 3
			return map[string]any{"status": "ok", "iface": "eth0", "attached": true, "configDir": cwDir}, nil
		case method == "GET" && p == "/api/v1/ports":
			return map[string]any{"tcp_in": ports.TCPIn, "udp_in": ports.UDPIn, "tcp_out": ports.TCPOut, "udp_out": ports.UDPOut, "allow_localhost": ports.AllowLocalhost}, nil
		case method == "PUT" && p == "/api/v1/ports":
			tin := ints(in["tcp_in"])
			if !contains2(tin, 22) && len(wl) <= 2 {
				return nil, invalid("SSH would be unreachable: keep port 22 in tcp_in or add your address to the allow list")
			}
			ports = &CWPorts{TCPIn: tin, UDPIn: ints(in["udp_in"]), TCPOut: ints(in["tcp_out"]), UDPOut: ints(in["udp_out"])}
			ports.AllowLocalhost, _ = in["allow_localhost"].(bool)
			return map[string]any{"status": "ok"}, nil
		case method == "GET" && p == "/api/v1/ips/whitelist":
			return map[string]any{"ips": wl}, nil
		case method == "GET" && p == "/api/v1/ips/blocklist":
			return map[string]any{"ips": bl}, nil
		case method == "POST" && p == "/api/v1/ips/whitelist":
			if ip, _ := in["ip"].(string); !contains(wl, ip) {
				wl = append(wl, ip)
			}
			return map[string]any{"status": "ok"}, nil
		case method == "POST" && p == "/api/v1/ips/blocklist":
			if ip, _ := in["ip"].(string); !contains(bl, ip) {
				bl = append(bl, ip)
			}
			return map[string]any{"status": "ok"}, nil
		case method == "DELETE" && strings.HasPrefix(p, "/api/v1/ips/whitelist/"):
			ip, _ := url.PathUnescape(strings.TrimPrefix(p, "/api/v1/ips/whitelist/"))
			wl = remove(wl, ip)
			return map[string]any{"status": "ok"}, nil
		case method == "DELETE" && strings.HasPrefix(p, "/api/v1/ips/blocklist/"):
			ip, _ := url.PathUnescape(strings.TrimPrefix(p, "/api/v1/ips/blocklist/"))
			bl = remove(bl, ip)
			return map[string]any{"status": "ok"}, nil
		case method == "GET" && p == "/api/v1/rules":
			out := []any{}
			for i, r := range rules {
				act := "pass"
				if r.file == "deny" {
					act = "drop"
				}
				out = append(out, map[string]any{"id": i, "rule": r.rule, "action": act, "file": r.file})
			}
			return map[string]any{"rules": out}, nil
		case method == "POST" && p == "/api/v1/rules":
			r, _ := in["rule"].(string)
			f, _ := in["file"].(string)
			parts := strings.Split(r, "|")
			if len(parts) < 3 || (parts[0] != "tcp" && parts[0] != "udp" && parts[0] != "icmp") || (parts[1] != "in" && parts[1] != "out") {
				return nil, invalid("rule must look like tcp|in|d=80|s=203.0.113.5")
			}
			if f == "" {
				f = "allow"
			}
			rules = append(rules, rule{r, f})
			return map[string]any{"status": "ok"}, nil
		case method == "DELETE" && strings.HasPrefix(p, "/api/v1/rules/"):
			var idx int
			fmt.Sscanf(strings.TrimPrefix(p, "/api/v1/rules/"), "%d", &idx)
			if idx < 0 || idx >= len(rules) {
				return nil, fmt.Errorf("firewall: no such rule")
			}
			rules = append(rules[:idx], rules[idx+1:]...)
			return map[string]any{"status": "ok"}, nil
		case method == "POST" && p == "/api/v1/reload":
			return map[string]any{"status": "ok"}, nil
		case method == "GET" && p == "/api/v1/stats":
			return map[string]any{"xdp": map[string]uint64{"pass": xdpPass, "drop": xdpDrop}, "tc": map[string]uint64{"pass": 20311, "drop": 0}}, nil
		case method == "GET" && p == "/api/v1/drops":
			d := []any{
				map[string]any{"ip": "185.224.128.17", "reason": "port_not_allowed", "proto": "tcp", "packets": 412, "last_dst_port": 3306, "last_seen": time.Now().Add(-4 * time.Minute).UTC().Format(time.RFC3339)},
				map[string]any{"ip": "45.141.84.210", "reason": "port_not_allowed", "proto": "tcp", "packets": 97, "last_dst_port": 23, "last_seen": time.Now().Add(-21 * time.Minute).UTC().Format(time.RFC3339)},
			}
			return map[string]any{"drops": d, "count": len(d)}, nil
		}
		return nil, fmt.Errorf("firewall: unsupported %s %s", method, path)
	}
	// Answer like the real daemon: everything passes through JSON.
	return func(method, path string, body any) (map[string]any, error) {
		out, err := handle(method, path, body)
		if err != nil {
			return nil, err
		}
		b, _ := json.Marshal(out)
		res := map[string]any{}
		_ = json.Unmarshal(b, &res)
		return res, nil
	}
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func contains2(l []int, n int) bool {
	for _, x := range l {
		if x == n {
			return true
		}
	}
	return false
}

func remove(l []string, s string) []string {
	out := []string{}
	for _, x := range l {
		if x != s {
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}
