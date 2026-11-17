package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"hostingpanel/agent/internal/api"
	"hostingpanel/agent/internal/runtimes"
	"hostingpanel/agent/internal/sites"
	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/system"
	"hostingpanel/agent/internal/tasks"
)

func main() {
	sock := flag.String("socket", "/run/hostingpanel/core.sock", "unix socket path")
	dbPath := flag.String("db", "/var/lib/hostingpanel/core.db", "sqlite state file")
	php := flag.String("default-php", "8.3", "default PHP version for new sites")
	dry := flag.String("dry-run-root", "", "if set, write all files under this dir and only log host commands (development)")
	group := flag.Int("socket-gid", -1, "gid that may access the socket (-1: leave as is)")
	flag.Parse()

	var run system.Runner = system.ExecRunner{}
	paths := sites.DefaultPaths("")
	if *dry != "" {
		run = &system.DryRunner{}
		paths = sites.DefaultPaths(*dry)
	}
	for _, d := range []string{filepath.Dir(*sock), filepath.Dir(*dbPath)} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			fatal(err)
		}
	}
	st, err := store.Open(*dbPath)
	if err != nil {
		fatal(err)
	}
	defer st.Close()

	_ = os.Remove(*sock)
	ln, err := net.Listen("unix", *sock)
	if err != nil {
		fatal(err)
	}
	_ = os.Chmod(*sock, 0o660)
	if *group >= 0 {
		_ = os.Chown(*sock, -1, *group)
	}

	tm := tasks.New(st)
	rt := runtimes.New(run, tm, st, paths.PHPDir)
	ss := sites.New(st, run, tm, paths, *php)
	if *dry == "" {
		ss.RuntimeInstalled = rt.Installed
		ss.Chown = chownTo
		if err := ss.EnsureSSHConfig(); err != nil {
			slog.Error("ssh config", "err", err)
		}
	}
	srv := &http.Server{Handler: (&api.Server{Sites: ss, Runtimes: rt, Tasks: tm}).Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		slog.Info("core-agent listening", "socket", *sock)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			fatal(err)
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	sctx, c := context.WithTimeout(context.Background(), 10*time.Second)
	defer c()
	_ = srv.Shutdown(sctx)
}

func chownTo(name, path string) {
	u, err := user.Lookup(name)
	if err != nil {
		return
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	_ = os.Lchown(path, uid, gid)
}

func fatal(err error) { slog.Error("fatal", "err", err); os.Exit(1) }
