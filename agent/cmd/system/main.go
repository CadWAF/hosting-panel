// system-service: monitoring, software store, firewall, fail2ban, systemd, updates, cron, logs, Docker and FTP.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/sysmgr"
	"hostingpanel/agent/internal/system"
	"hostingpanel/agent/internal/tasks"
)

func main() {
	sock := flag.String("socket", "/run/hostingpanel/system.sock", "unix socket path")
	state := flag.String("state", "/var/lib/hostingpanel/system", "state directory")
	sitesBase := flag.String("sites-base", "/srv/sites", "directory holding site homes")
	dry := flag.Bool("dry-run", false, "simulate the host: nothing real is touched (development)")
	flag.Parse()

	_ = os.MkdirAll(filepath.Dir(*sock), 0o750)
	_ = os.MkdirAll(*state, 0o700)
	st, err := store.Open(filepath.Join(*state, "system.db"))
	must(err)
	defer st.Close()
	tm := tasks.New(st)

	var run system.Runner = system.ExecRunner{}
	root := ""
	if *dry {
		root = filepath.Join(*state, "root")
		run = sysmgr.NewSim(root)
	}
	m := sysmgr.New(run, tm, root)
	m.SitesBase = *sitesBase
	if *dry {
		m.CW = sysmgr.NewCWFake(m)
		m.Lookup = func(name string) (int, int, string, error) { return 1001, 1001, filepath.Join(*sitesBase, name), nil }
	}

	_ = os.Remove(*sock)
	ln, err := net.Listen("unix", *sock)
	must(err)
	_ = os.Chmod(*sock, 0o660)
	srv := &http.Server{Handler: m.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	stop := make(chan struct{})
	go m.RunSampler(stop)
	slog.Info("system-service listening", "socket", *sock, "dry_run", *dry)

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
