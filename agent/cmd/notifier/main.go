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

	"hostingpanel/agent/internal/notifier"
)

func main() {
	cfgPath := flag.String("config", "/etc/hostingpanel/notifier.json", "config file")
	sock := flag.String("socket", "/run/hostingpanel/notifier.sock", "unix socket for events/status")
	flag.Parse()

	cfg, err := notifier.LoadConfig(*cfgPath)
	if err != nil {
		slog.Error("config", "err", err)
		os.Exit(1)
	}
	var senders []notifier.Sender
	if t := cfg.Telegram; t != nil && t.Token != "" {
		senders = append(senders, notifier.Telegram{Token: t.Token, ChatID: t.ChatID})
	}
	if s := cfg.SMTP; s != nil && s.Host != "" {
		senders = append(senders, notifier.Email{Host: s.Host, Port: s.Port, User: s.User, Pass: s.Pass, From: s.From, To: s.To})
	}
	if len(senders) == 0 {
		slog.Warn("no notification channel configured")
	}
	mon := notifier.NewMonitor(cfg, senders, nil)

	_ = os.MkdirAll(filepath.Dir(*sock), 0o750)
	_ = os.Remove(*sock)
	ln, err := net.Listen("unix", *sock)
	if err != nil {
		slog.Error("listen", "err", err)
		os.Exit(1)
	}
	_ = os.Chmod(*sock, 0o660)
	srv := &http.Server{Handler: mon.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	mon.Notify("ℹ️ Notifier started", "Monitoring is active.")
	mon.Run(ctx)
	_ = srv.Shutdown(context.Background())
}
