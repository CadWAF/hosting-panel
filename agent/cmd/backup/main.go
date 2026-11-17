package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"hostingpanel/agent/internal/backup"
	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/system"
	"hostingpanel/agent/internal/tasks"
)

func main() {
	sock := flag.String("socket", "/run/hostingpanel/backup.sock", "unix socket path")
	state := flag.String("state", "/var/lib/hostingpanel/backup", "state directory (sqlite, secrets, staging)")
	notifier := flag.String("notifier-socket", "/run/hostingpanel/notifier.sock", "notifier socket for failure alerts")
	sitesBase := flag.String("sites-base", "/srv/sites", "directory holding site homes")
	dry := flag.Bool("dry-run", false, "log host commands instead of running them")
	flag.Parse()

	var run system.Runner = system.ExecRunner{}
	if *dry {
		run = &system.DryRunner{}
	}
	_ = os.MkdirAll(filepath.Dir(*sock), 0o750)
	_ = os.MkdirAll(*state, 0o700)
	st, err := store.Open(filepath.Join(*state, "backup.db"))
	must(err)
	defer st.Close()
	tm := tasks.New(st)
	m, err := backup.New(st, run, tm, *state)
	must(err)
	m.SitesBase = *sitesBase
	m.Notify = func(level, title, msg string) { notify(*notifier, level, title, msg) }

	_ = os.Remove(*sock)
	ln, err := net.Listen("unix", *sock)
	must(err)
	_ = os.Chmod(*sock, 0o660)
	srv := &http.Server{Handler: handler(m, tm), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	stop := make(chan struct{})
	go m.RunScheduler(stop)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	<-ctx.Done()
	close(stop)
	_ = srv.Shutdown(context.Background())
}

func must(err error) {
	if err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// notify tells the Notifier; if it is down the alert is simply lost, never blocking the backup.
func notify(socket, level, title, msg string) {
	c := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}}
	b, _ := json.Marshal(map[string]string{"level": level, "title": title, "message": msg})
	resp, err := c.Post("http://notifier/v1/events", "application/json", bytes.NewReader(b))
	if err != nil {
		slog.Warn("notifier unreachable", "err", err)
		return
	}
	resp.Body.Close()
}

func handler(m *backup.Manager, tm *tasks.Manager) http.Handler {
	mux := http.NewServeMux()
	out := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	fail := func(w http.ResponseWriter, err error) {
		code := 500
		switch {
		case errors.Is(err, backup.ErrInvalid):
			code = 422
		case errors.Is(err, backup.ErrExists):
			code = 409
		case errors.Is(err, backup.ErrNotFound), errors.Is(err, store.ErrNotFound):
			code = 404
		}
		out(w, code, map[string]string{"error": err.Error()})
	}
	id := func(r *http.Request) int64 { n, _ := strconv.ParseInt(r.PathValue("id"), 10, 64); return n }
	body := func(w http.ResponseWriter, r *http.Request, v any) bool {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(v); err != nil {
			out(w, 400, map[string]string{"error": "bad json"})
			return false
		}
		return true
	}
	data := func(w http.ResponseWriter, code int, v any, err error) {
		if err != nil {
			fail(w, err)
			return
		}
		out(w, code, map[string]any{"data": v})
	}

	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		out(w, 200, map[string]string{"status": "ok", "service": "backup"})
	})
	mux.HandleFunc("GET /v1/destinations", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Destinations()
		data(w, 200, v, err)
	})
	mux.HandleFunc("POST /v1/destinations", func(w http.ResponseWriter, r *http.Request) {
		var in backup.DestinationInput
		if !body(w, r, &in) {
			return
		}
		v, err := m.AddDestination(in)
		data(w, 201, v, err)
	})
	mux.HandleFunc("POST /v1/destinations/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		data(w, 200, map[string]bool{"ok": true}, m.TestDestination(id(r)))
	})
	mux.HandleFunc("DELETE /v1/destinations/{id}", func(w http.ResponseWriter, r *http.Request) {
		data(w, 200, nil, m.DeleteDestination(id(r)))
	})
	mux.HandleFunc("GET /v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Jobs()
		data(w, 200, v, err)
	})
	mux.HandleFunc("POST /v1/jobs", func(w http.ResponseWriter, r *http.Request) {
		var in backup.Job
		if !body(w, r, &in) {
			return
		}
		v, err := m.AddJob(in)
		data(w, 201, v, err)
	})
	mux.HandleFunc("DELETE /v1/jobs/{id}", func(w http.ResponseWriter, r *http.Request) { data(w, 200, nil, m.DeleteJob(id(r))) })
	mux.HandleFunc("POST /v1/jobs/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.RunJob(id(r))
		data(w, 202, v, err)
	})
	mux.HandleFunc("GET /v1/runs", func(w http.ResponseWriter, r *http.Request) {
		j, _ := strconv.ParseInt(r.URL.Query().Get("job"), 10, 64)
		v, err := m.Runs(j)
		data(w, 200, v, err)
	})
	mux.HandleFunc("POST /v1/runs/{id}/restore", func(w http.ResponseWriter, r *http.Request) {
		var in backup.RestoreInput
		if !body(w, r, &in) {
			return
		}
		v, err := m.Restore(id(r), in)
		data(w, 202, v, err)
	})
	mux.HandleFunc("POST /v1/borg/install", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.InstallBorg()
		data(w, 202, v, err)
	})
	mux.HandleFunc("GET /v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, err := tm.Get(r.PathValue("id"))
		data(w, 200, v, err)
	})
	return mux
}
