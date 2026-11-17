// Package notifier watches Services and tells the Administrator when something breaks or recovers.
// It deliberately depends on nothing but the standard library and its own config file.
package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Telegram *struct {
		Token  string `json:"token"`
		ChatID string `json:"chat_id"`
	} `json:"telegram"`
	SMTP *struct {
		Host string `json:"host"`
		Port int    `json:"port"`
		User string `json:"user"`
		Pass string `json:"pass"`
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"smtp"`
	IntervalSeconds int     `json:"interval_seconds"`
	FailThreshold   int     `json:"fail_threshold"`
	DiskPercent     float64 `json:"disk_percent"` // alert when used % >= this (0 disables)
	Checks          []Check `json:"checks"`
}

// Check kinds: unix-http (target = "socket|/path"), http (URL), systemd (unit), tcp (host:port).
type Check struct {
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Target string `json:"target"`
}

func LoadConfig(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	if c.IntervalSeconds <= 0 {
		c.IntervalSeconds = 30
	}
	if c.FailThreshold <= 0 {
		c.FailThreshold = 2
	}
	return c, nil
}

// Sender delivers one message to the Administrator.
type Sender interface {
	Send(title, body string) error
	Name() string
}

type Message struct{ Title, Body string }

type Telegram struct{ Token, ChatID, BaseURL string }

func (t Telegram) Name() string { return "telegram" }
func (t Telegram) Send(title, body string) error {
	base := t.BaseURL
	if base == "" {
		base = "https://api.telegram.org"
	}
	payload, _ := json.Marshal(map[string]string{"chat_id": t.ChatID, "text": title + "\n" + body})
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Post(base+"/bot"+t.Token+"/sendMessage", "application/json", strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram: status %d", resp.StatusCode)
	}
	return nil
}

type Email struct {
	Host                 string
	Port                 int
	User, Pass, From, To string
}

func (e Email) Name() string { return "email" }
func (e Email) Send(title, body string) error {
	addr := fmt.Sprintf("%s:%d", e.Host, e.Port)
	msg := "From: " + e.From + "\r\nTo: " + e.To + "\r\nSubject: " + strings.NewReplacer("\r", " ", "\n", " ").Replace(title) +
		"\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body + "\r\n"
	var auth smtp.Auth
	if e.User != "" {
		auth = smtp.PlainAuth("", e.User, e.Pass, e.Host)
	}
	return smtp.SendMail(addr, auth, e.From, []string{e.To}, []byte(msg))
}

// Runner evaluates a Check; nil error means healthy.
type CheckFunc func(ctx context.Context, c Check) error

func DefaultCheck(ctx context.Context, c Check) error {
	switch c.Kind {
	case "unix-http":
		parts := strings.SplitN(c.Target, "|", 2)
		if len(parts) != 2 {
			return fmt.Errorf("bad target")
		}
		cl := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", parts[0])
			}}}
		return httpOK(ctx, cl, "http://unix"+parts[1])
	case "http":
		return httpOK(ctx, &http.Client{Timeout: 8 * time.Second}, c.Target)
	case "tcp":
		conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", c.Target)
		if err != nil {
			return err
		}
		return conn.Close()
	case "systemd":
		if out, err := exec.CommandContext(ctx, "systemctl", "is-active", c.Target).CombinedOutput(); err != nil {
			return fmt.Errorf("%s", strings.TrimSpace(string(out)))
		}
		return nil
	}
	return fmt.Errorf("unknown check kind %q", c.Kind)
}

func httpOK(ctx context.Context, cl *http.Client, url string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

type CheckState struct {
	Name      string    `json:"name"`
	Up        bool      `json:"up"`
	Fails     int       `json:"fails"`
	LastError string    `json:"last_error"`
	Since     time.Time `json:"since"`
}

type Monitor struct {
	cfg     Config
	senders []Sender
	check   CheckFunc
	now     func() time.Time

	mu       sync.Mutex
	states   map[string]*CheckState
	diskHi   bool
	DiskUsed func() (float64, error)
}

func NewMonitor(cfg Config, senders []Sender, check CheckFunc) *Monitor {
	if check == nil {
		check = DefaultCheck
	}
	m := &Monitor{cfg: cfg, senders: senders, check: check, now: time.Now, states: map[string]*CheckState{}, DiskUsed: diskUsed}
	for _, c := range cfg.Checks {
		m.states[c.Name] = &CheckState{Name: c.Name, Up: true, Since: m.now()}
	}
	return m
}

// Notify sends to every configured channel; one channel failing never blocks the others.
func (m *Monitor) Notify(title, body string) {
	for _, s := range m.senders {
		if err := s.Send(title, body); err != nil {
			slog.Error("send failed", "channel", s.Name(), "err", err)
		}
	}
}

// Tick runs all checks once and notifies on state changes.
func (m *Monitor) Tick(ctx context.Context) {
	for _, c := range m.cfg.Checks {
		err := m.check(ctx, c)
		m.mu.Lock()
		st := m.states[c.Name]
		var note *Message
		if err == nil {
			if !st.Up {
				note = &Message{"✅ " + c.Name + " is back", fmt.Sprintf("Recovered after %s.", m.now().Sub(st.Since).Round(time.Second))}
				st.Since = m.now()
			}
			st.Up, st.Fails, st.LastError = true, 0, ""
		} else {
			st.Fails++
			st.LastError = err.Error()
			if st.Up && st.Fails >= m.cfg.FailThreshold {
				st.Up, st.Since = false, m.now()
				note = &Message{"🔴 " + c.Name + " is down", "Error: " + err.Error()}
			}
		}
		m.mu.Unlock()
		if note != nil {
			m.Notify(note.Title, note.Body)
		}
	}
	m.checkDisk()
}

func (m *Monitor) checkDisk() {
	if m.cfg.DiskPercent <= 0 || m.DiskUsed == nil {
		return
	}
	used, err := m.DiskUsed()
	if err != nil {
		return
	}
	m.mu.Lock()
	hi := used >= m.cfg.DiskPercent
	changed := hi != m.diskHi
	m.diskHi = hi
	m.mu.Unlock()
	if changed && hi {
		m.Notify("🟠 Disk almost full", fmt.Sprintf("Root filesystem is %.0f%% used.", used))
	} else if changed {
		m.Notify("✅ Disk usage back to normal", fmt.Sprintf("Root filesystem is %.0f%% used.", used))
	}
}

func (m *Monitor) Status() []CheckState {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []CheckState{}
	for _, c := range m.cfg.Checks {
		out = append(out, *m.states[c.Name])
	}
	return out
}

// Handler serves GET /v1/status, GET /v1/health and POST /v1/events (from other Services).
func (m *Monitor) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok", "service": "notifier"})
	})
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": m.Status()})
	})
	mux.HandleFunc("POST /v1/events", func(w http.ResponseWriter, r *http.Request) {
		var e struct{ Level, Title, Message string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<15)).Decode(&e); err != nil || e.Title == "" {
			http.Error(w, `{"error":"title required"}`, 422)
			return
		}
		icon := map[string]string{"error": "🔴 ", "warning": "🟠 ", "info": "ℹ️ "}[e.Level]
		m.Notify(icon+e.Title, e.Message)
		w.WriteHeader(202)
	})
	return mux
}

// Run ticks until ctx ends.
func (m *Monitor) Run(ctx context.Context) {
	t := time.NewTicker(time.Duration(m.cfg.IntervalSeconds) * time.Second)
	defer t.Stop()
	m.Tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Tick(ctx)
		}
	}
}
