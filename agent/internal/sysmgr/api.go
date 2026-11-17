package sysmgr

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"hostingpanel/agent/internal/store"
)

func respond(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func fail(w http.ResponseWriter, err error) {
	code := 500
	switch {
	case errors.Is(err, ErrInvalid):
		code = 422
	case errors.Is(err, ErrNotFound), errors.Is(err, store.ErrNotFound):
		code = 404
	case errors.Is(err, ErrConflict):
		code = 409
	}
	respond(w, code, map[string]string{"error": err.Error()})
}

// done answers a mutation: 200 with the value, or the mapped error.
func done(w http.ResponseWriter, code int, v any, err error) {
	if err != nil {
		fail(w, err)
		return
	}
	respond(w, code, map[string]any{"data": v})
}

func body(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(v); err != nil {
		respond(w, 400, map[string]string{"error": "bad json"})
		return false
	}
	return true
}

func intq(r *http.Request, k string, def int) int {
	if n, err := strconv.Atoi(r.URL.Query().Get(k)); err == nil {
		return n
	}
	return def
}

// Handler is the System Service's HTTP API (served on a Unix socket).
func (m *Manager) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]string{"status": "ok", "service": "system"})
	})
	mux.HandleFunc("GET /v1/stats", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]any{"data": m.Stats()}) })

	// Software store and systemd units.
	mux.HandleFunc("GET /v1/software", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"data": m.Software(), "categories": Categories()})
	})
	mux.HandleFunc("POST /v1/software/{id}/install", func(w http.ResponseWriter, r *http.Request) {
		t, err := m.InstallSoftware(r.PathValue("id"))
		done(w, 202, t, err)
	})
	mux.HandleFunc("POST /v1/software/{id}/remove", func(w http.ResponseWriter, r *http.Request) {
		t, err := m.RemoveSoftware(r.PathValue("id"))
		done(w, 202, t, err)
	})
	mux.HandleFunc("GET /v1/services", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"data": m.Services(r.URL.Query().Get("all") == "1")})
	})
	mux.HandleFunc("POST /v1/services/{unit}/{action}", func(w http.ResponseWriter, r *http.Request) {
		done(w, 200, nil, m.ServiceAction(r.PathValue("unit"), r.PathValue("action")))
	})

	// Security.
	mux.HandleFunc("GET /v1/fail2ban", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]any{"data": m.Fail2ban()}) })
	mux.HandleFunc("PUT /v1/fail2ban/defaults", func(w http.ResponseWriter, r *http.Request) {
		var in DefaultsInput
		if body(w, r, &in) {
			done(w, 200, nil, m.SetDefaults(in))
		}
	})
	mux.HandleFunc("PUT /v1/fail2ban/jails/{name}", func(w http.ResponseWriter, r *http.Request) {
		var in JailInput
		if body(w, r, &in) {
			done(w, 200, nil, m.SetJail(r.PathValue("name"), in))
		}
	})
	mux.HandleFunc("POST /v1/fail2ban/jails/{name}/ban", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IP string `json:"ip"`
		}
		if body(w, r, &in) {
			done(w, 200, nil, m.Ban(r.PathValue("name"), in.IP))
		}
	})
	mux.HandleFunc("DELETE /v1/fail2ban/jails/{name}/ban/{ip}", func(w http.ResponseWriter, r *http.Request) {
		done(w, 200, nil, m.Unban(r.PathValue("name"), r.PathValue("ip")))
	})
	mux.HandleFunc("GET /v1/firewall", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]any{"data": m.Firewall()}) })
	mux.HandleFunc("POST /v1/firewall/enable", func(w http.ResponseWriter, r *http.Request) { done(w, 200, nil, m.EnableFirewall()) })
	mux.HandleFunc("POST /v1/firewall/disable", func(w http.ResponseWriter, r *http.Request) { done(w, 200, nil, m.DisableFirewall()) })
	mux.HandleFunc("POST /v1/firewall/rules", func(w http.ResponseWriter, r *http.Request) {
		var in RuleInput
		if body(w, r, &in) {
			done(w, 201, nil, m.AddRule(in))
		}
	})
	mux.HandleFunc("POST /v1/firewall/rules/delete", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			N    int    `json:"n"`
			Spec string `json:"spec"`
		}
		if body(w, r, &in) {
			done(w, 200, nil, m.DeleteRule(in.N, in.Spec))
		}
	})
	mux.HandleFunc("GET /v1/ssh", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]any{"data": m.SSH()}) })
	mux.HandleFunc("PUT /v1/ssh", func(w http.ResponseWriter, r *http.Request) {
		var in SSHInput
		if body(w, r, &in) {
			done(w, 200, nil, m.SetSSH(in))
		}
	})

	// Updates.
	mux.HandleFunc("GET /v1/updates", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]any{"data": m.Updates()}) })
	mux.HandleFunc("POST /v1/updates/check", func(w http.ResponseWriter, r *http.Request) {
		t, err := m.CheckUpdates()
		done(w, 202, t, err)
	})
	mux.HandleFunc("POST /v1/updates/apply", func(w http.ResponseWriter, r *http.Request) {
		t, err := m.ApplyUpdates()
		done(w, 202, t, err)
	})
	mux.HandleFunc("POST /v1/reboot", func(w http.ResponseWriter, r *http.Request) { done(w, 202, nil, m.Reboot()) })

	// Scheduled jobs.
	mux.HandleFunc("GET /v1/cron", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]any{"data": m.CronJobs()}) })
	mux.HandleFunc("POST /v1/cron", func(w http.ResponseWriter, r *http.Request) {
		var in CronJob
		if body(w, r, &in) {
			j, err := m.AddCron(in)
			done(w, 201, j, err)
		}
	})
	mux.HandleFunc("PUT /v1/cron/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in CronJob
		if body(w, r, &in) {
			done(w, 200, nil, m.UpdateCron(r.PathValue("id"), in))
		}
	})
	mux.HandleFunc("DELETE /v1/cron/{id}", func(w http.ResponseWriter, r *http.Request) { done(w, 200, nil, m.DeleteCron(r.PathValue("id"))) })
	mux.HandleFunc("POST /v1/cron/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		t, err := m.RunCron(r.PathValue("id"))
		done(w, 202, t, err)
	})

	// Logs.
	mux.HandleFunc("GET /v1/logs", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]any{"data": m.Logs()}) })
	mux.HandleFunc("GET /v1/logs/read", func(w http.ResponseWriter, r *http.Request) {
		s, err := m.ReadLog(r.URL.Query().Get("id"), intq(r, "lines", 300), r.URL.Query().Get("q"))
		done(w, 200, s, err)
	})

	// Docker.
	mux.HandleFunc("GET /v1/docker", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]any{"data": m.Docker()}) })
	mux.HandleFunc("POST /v1/docker/containers", func(w http.ResponseWriter, r *http.Request) {
		var in RunInput
		if body(w, r, &in) {
			t, err := m.RunContainer(in)
			done(w, 202, t, err)
		}
	})
	mux.HandleFunc("POST /v1/docker/containers/{id}/{action}", func(w http.ResponseWriter, r *http.Request) {
		done(w, 200, nil, m.ContainerAction(r.PathValue("id"), r.PathValue("action")))
	})
	mux.HandleFunc("GET /v1/docker/containers/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		s, err := m.ContainerLog(r.PathValue("id"), intq(r, "lines", 200))
		done(w, 200, s, err)
	})
	mux.HandleFunc("POST /v1/docker/images/pull", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Image string `json:"image"`
		}
		if body(w, r, &in) {
			t, err := m.PullImage(in.Image)
			done(w, 202, t, err)
		}
	})
	mux.HandleFunc("DELETE /v1/docker/images/{id}", func(w http.ResponseWriter, r *http.Request) { done(w, 200, nil, m.RemoveImage(r.PathValue("id"))) })

	// FTP.
	mux.HandleFunc("GET /v1/ftp", func(w http.ResponseWriter, r *http.Request) { respond(w, 200, map[string]any{"data": m.FTP()}) })
	mux.HandleFunc("POST /v1/ftp/accounts", func(w http.ResponseWriter, r *http.Request) {
		var in FTPInput
		if body(w, r, &in) {
			done(w, 201, nil, m.AddFTP(in))
		}
	})
	mux.HandleFunc("PUT /v1/ftp/accounts/{name}/password", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Password string `json:"password"`
		}
		if body(w, r, &in) {
			done(w, 200, nil, m.SetFTPPassword(r.PathValue("name"), in.Password))
		}
	})
	mux.HandleFunc("DELETE /v1/ftp/accounts/{name}", func(w http.ResponseWriter, r *http.Request) { done(w, 200, nil, m.DeleteFTP(r.PathValue("name"))) })
	mux.HandleFunc("POST /v1/ftp/open-ports", func(w http.ResponseWriter, r *http.Request) { done(w, 200, nil, m.OpenFTPPorts()) })

	// Tasks.
	mux.HandleFunc("GET /v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		l, err := m.Tasks.List()
		done(w, 200, l, err)
	})
	mux.HandleFunc("GET /v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		t, err := m.Tasks.Get(r.PathValue("id"))
		done(w, 200, t, err)
	})
	return mux
}
