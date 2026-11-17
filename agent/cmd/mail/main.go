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
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"hostingpanel/agent/internal/mail"
	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/system"
	"hostingpanel/agent/internal/tasks"
)

func main() {
	sock := flag.String("socket", "/run/hostingpanel/mail.sock", "panel-facing unix socket")
	ssoSock := flag.String("sso-socket", mail.SSOSocket, "webmail token redemption socket (readable by the web server)")
	state := flag.String("state", "/var/lib/hostingpanel/mail", "state directory")
	root := flag.String("mail-root", "/var/mail/vhosts", "maildir root")
	dry := flag.Bool("dry-run", false, "log host commands instead of running them")
	flag.Parse()

	var run system.Runner = system.ExecRunner{}
	if *dry {
		run = &system.DryRunner{}
	}
	_ = os.MkdirAll(*state, 0o750)
	_ = os.MkdirAll(filepath.Dir(*sock), 0o750)
	st, err := store.Open(filepath.Join(*state, "tasks.db"))
	must(err)
	defer st.Close()
	tm := tasks.New(st)
	m, err := mail.New(mail.Config{StateDir: *state, MailRoot: *root}, run)
	must(err)
	defer m.Close()

	serve := func(path string, mode os.FileMode, group string, h http.Handler) *http.Server {
		_ = os.Remove(path)
		ln, err := net.Listen("unix", path)
		must(err)
		_ = os.Chmod(path, mode)
		if g, err := user.LookupGroup(group); err == nil {
			gid, _ := strconv.Atoi(g.Gid)
			_ = os.Chown(path, -1, gid)
		}
		s := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = s.Serve(ln) }()
		return s
	}
	main := serve(*sock, 0o660, "hostingpanel", handler(m, tm))
	ssoDir := filepath.Dir(*ssoSock)
	_ = os.MkdirAll(ssoDir, 0o750)
	_ = os.Chmod(ssoDir, 0o750)
	if g, err := user.LookupGroup("www-data"); err == nil {
		gid, _ := strconv.Atoi(g.Gid)
		_ = os.Chown(ssoDir, -1, gid)
	}
	sso := serve(*ssoSock, 0o660, "www-data", ssoHandler(m))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	_ = main.Shutdown(context.Background())
	_ = sso.Shutdown(context.Background())
}

func must(err error) {
	if err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// ssoHandler exposes only token redemption: the web server can redeem, never list or create.
func ssoHandler(m *mail.Manager) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sso/consume", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Token string }
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in) != nil {
			http.Error(w, `{"error":"bad json"}`, 400)
			return
		}
		email, err := m.ConsumeSSO(in.Token)
		if err != nil {
			http.Error(w, `{"error":"invalid or expired token"}`, 403)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"email": email})
	})
	return mux
}

func handler(m *mail.Manager, tm *tasks.Manager) http.Handler {
	mux := http.NewServeMux()
	out := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	data := func(w http.ResponseWriter, code int, v any, err error) {
		if err != nil {
			c := 500
			switch {
			case errors.Is(err, mail.ErrInvalid):
				c = 422
			case errors.Is(err, mail.ErrExists):
				c = 409
			case errors.Is(err, mail.ErrNotFound), errors.Is(err, store.ErrNotFound):
				c = 404
			}
			out(w, c, map[string]string{"error": err.Error()})
			return
		}
		out(w, code, map[string]any{"data": v})
	}
	body := func(w http.ResponseWriter, r *http.Request, v any) bool {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<14)).Decode(v); err != nil {
			out(w, 400, map[string]string{"error": "bad json"})
			return false
		}
		return true
	}
	id := func(r *http.Request) int64 { n, _ := strconv.ParseInt(r.PathValue("id"), 10, 64); return n }

	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		out(w, 200, map[string]string{"status": "ok", "service": "mail"})
	})
	mux.HandleFunc("GET /v1/settings", func(w http.ResponseWriter, r *http.Request) { data(w, 200, m.Settings(), nil) })
	mux.HandleFunc("PUT /v1/settings", func(w http.ResponseWriter, r *http.Request) {
		var s mail.Settings
		if !body(w, r, &s) {
			return
		}
		err := m.SetSettings(s)
		data(w, 200, m.Settings(), err)
	})
	mux.HandleFunc("POST /v1/install", func(w http.ResponseWriter, r *http.Request) {
		t, err := m.InstallStack(tm, m.Settings().PHPSocket)
		data(w, 202, t, err)
	})
	mux.HandleFunc("GET /v1/domains", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Domains()
		data(w, 200, v, err)
	})
	mux.HandleFunc("POST /v1/domains", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		if !body(w, r, &in) {
			return
		}
		v, err := m.AddDomain(in.Name)
		data(w, 201, v, err)
	})
	mux.HandleFunc("DELETE /v1/domains/{name}", func(w http.ResponseWriter, r *http.Request) {
		data(w, 200, nil, m.DeleteDomain(r.PathValue("name")))
	})
	mux.HandleFunc("GET /v1/domains/{name}/dns", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Verify(r.Context(), r.PathValue("name"), mail.SystemResolver{})
		data(w, 200, v, err)
	})
	mux.HandleFunc("GET /v1/mailboxes", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Mailboxes(r.URL.Query().Get("domain"))
		data(w, 200, v, err)
	})
	mux.HandleFunc("POST /v1/mailboxes", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Email, Password string }
		if !body(w, r, &in) {
			return
		}
		v, err := m.AddMailbox(in.Email, in.Password)
		data(w, 201, v, err)
	})
	mux.HandleFunc("PUT /v1/mailboxes/{id}/password", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Password string }
		if !body(w, r, &in) {
			return
		}
		data(w, 200, nil, m.SetPassword(id(r), in.Password))
	})
	mux.HandleFunc("DELETE /v1/mailboxes/{id}", func(w http.ResponseWriter, r *http.Request) { data(w, 200, nil, m.DeleteMailbox(id(r))) })
	mux.HandleFunc("POST /v1/mailboxes/{id}/webmail", func(w http.ResponseWriter, r *http.Request) {
		u, err := m.IssueSSO(id(r))
		data(w, 200, map[string]string{"url": u}, err)
	})
	mux.HandleFunc("GET /v1/aliases", func(w http.ResponseWriter, r *http.Request) {
		v, err := m.Aliases()
		data(w, 200, v, err)
	})
	mux.HandleFunc("POST /v1/aliases", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Source, Destination string }
		if !body(w, r, &in) {
			return
		}
		v, err := m.AddAlias(in.Source, in.Destination)
		data(w, 201, v, err)
	})
	mux.HandleFunc("DELETE /v1/aliases/{id}", func(w http.ResponseWriter, r *http.Request) { data(w, 200, nil, m.DeleteAlias(id(r))) })
	mux.HandleFunc("GET /v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		v, err := tm.Get(r.PathValue("id"))
		data(w, 200, v, err)
	})
	return mux
}
