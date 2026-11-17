package main

import (
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

	"hostingpanel/agent/internal/dbmanager"
	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/system"
	"hostingpanel/agent/internal/tasks"
)

func main() {
	sock := flag.String("socket", "/run/hostingpanel/db.sock", "unix socket path")
	dbPath := flag.String("db", "/var/lib/hostingpanel/db.db", "sqlite state file")
	dry := flag.Bool("dry-run", false, "log host commands instead of running them (development)")
	flag.Parse()

	var run system.Runner = system.ExecRunner{}
	if *dry {
		run = &system.DryRunner{}
	}
	for _, d := range []string{filepath.Dir(*sock), filepath.Dir(*dbPath)} {
		_ = os.MkdirAll(d, 0o750)
	}
	st, err := store.Open(*dbPath)
	must(err)
	defer st.Close()
	tm := tasks.New(st)
	m, err := dbmanager.New(run, tm, st)
	must(err)
	if *dry {
		m.Installed = func(string) bool { return true }
	}

	_ = os.Remove(*sock)
	ln, err := net.Listen("unix", *sock)
	must(err)
	_ = os.Chmod(*sock, 0o660)
	srv := &http.Server{Handler: handler(m, tm), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	_ = srv.Shutdown(context.Background())
}

func must(err error) {
	if err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func handler(m *dbmanager.Manager, tm *tasks.Manager) http.Handler {
	mux := http.NewServeMux()
	out := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	fail := func(w http.ResponseWriter, err error) {
		code := 500
		switch {
		case errors.Is(err, dbmanager.ErrInvalid), errors.Is(err, dbmanager.ErrNoServer):
			code = 422
		case errors.Is(err, dbmanager.ErrExists):
			code = 409
		case errors.Is(err, dbmanager.ErrNotFound), errors.Is(err, store.ErrNotFound):
			code = 404
		}
		out(w, code, map[string]string{"error": err.Error()})
	}
	id := func(r *http.Request) int64 { n, _ := strconv.ParseInt(r.PathValue("id"), 10, 64); return n }

	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		out(w, 200, map[string]string{"status": "ok", "service": "dbmanager"})
	})
	mux.HandleFunc("GET /v1/engines", func(w http.ResponseWriter, r *http.Request) { out(w, 200, map[string]any{"data": m.Engines()}) })
	mux.HandleFunc("POST /v1/engines/{engine}/install", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Version string }
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in)
		t, err := m.InstallEngine(r.PathValue("engine"), in.Version)
		if err != nil {
			fail(w, err)
			return
		}
		out(w, 202, map[string]any{"data": t})
	})
	mux.HandleFunc("GET /v1/databases", func(w http.ResponseWriter, r *http.Request) {
		l, err := m.List()
		if err != nil {
			fail(w, err)
			return
		}
		out(w, 200, map[string]any{"data": l})
	})
	mux.HandleFunc("POST /v1/databases", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Engine, Name, Site string }
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12)).Decode(&in); err != nil {
			out(w, 400, map[string]string{"error": "bad json"})
			return
		}
		d, pass, err := m.Create(in.Engine, in.Name, in.Site)
		if err != nil {
			fail(w, err)
			return
		}
		out(w, 201, map[string]any{"data": d, "password": pass})
	})
	mux.HandleFunc("POST /v1/databases/{id}/password", func(w http.ResponseWriter, r *http.Request) {
		p, err := m.ResetPassword(id(r))
		if err != nil {
			fail(w, err)
			return
		}
		out(w, 200, map[string]any{"password": p})
	})
	mux.HandleFunc("DELETE /v1/databases/{id}", func(w http.ResponseWriter, r *http.Request) {
		if err := m.Delete(id(r)); err != nil {
			fail(w, err)
			return
		}
		out(w, 200, map[string]any{"data": nil})
	})
	mux.HandleFunc("GET /v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		t, err := tm.Get(r.PathValue("id"))
		if err != nil {
			fail(w, err)
			return
		}
		out(w, 200, map[string]any{"data": t})
	})
	return mux
}
