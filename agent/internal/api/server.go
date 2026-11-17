// Package api exposes the Core Agent as JSON over HTTP (served on a Unix socket).
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"hostingpanel/agent/internal/runtimes"
	"hostingpanel/agent/internal/sitefiles"
	"hostingpanel/agent/internal/sites"
	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/tasks"
)

type Server struct {
	Sites    *sites.Service
	Runtimes *runtimes.Manager
	Tasks    *tasks.Manager
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(v); err != nil {
		respond(w, 400, map[string]string{"error": "bad json"})
		return false
	}
	return true
}

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]string{"status": "ok", "service": "core-agent"})
	})
	m.HandleFunc("GET /v1/sites", func(w http.ResponseWriter, r *http.Request) {
		l, err := s.Sites.List()
		reply(w, 200, map[string]any{"data": l}, err)
	})
	m.HandleFunc("POST /v1/sites", func(w http.ResponseWriter, r *http.Request) {
		var in sites.CreateInput
		if !decode(w, r, &in) {
			return
		}
		x, err := s.Sites.Create(in)
		reply(w, 201, map[string]any{"data": x}, err)
	})
	m.HandleFunc("PATCH /v1/sites/{domain}", func(w http.ResponseWriter, r *http.Request) {
		var in sites.UpdateInput
		if !decode(w, r, &in) {
			return
		}
		x, err := s.Sites.Update(r.PathValue("domain"), in)
		reply(w, 200, map[string]any{"data": x}, err)
	})
	m.HandleFunc("GET /v1/php/settings-keys", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"data": sites.AllowedSettingKeys()})
	})
	m.HandleFunc("GET /v1/php", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{"data": s.Runtimes.List(), "extensions": runtimes.Extensions})
	})
	m.HandleFunc("POST /v1/php/{version}/install", func(w http.ResponseWriter, r *http.Request) {
		t, err := s.Runtimes.Install(r.PathValue("version"))
		reply(w, 202, map[string]any{"data": t}, err)
	})
	m.HandleFunc("DELETE /v1/php/{version}", func(w http.ResponseWriter, r *http.Request) {
		t, err := s.Runtimes.Remove(r.PathValue("version"))
		reply(w, 202, map[string]any{"data": t}, err)
	})
	m.HandleFunc("POST /v1/php/{version}/extensions/{ext}", func(w http.ResponseWriter, r *http.Request) {
		t, err := s.Runtimes.InstallExtension(r.PathValue("version"), r.PathValue("ext"))
		reply(w, 202, map[string]any{"data": t}, err)
	})
	m.HandleFunc("DELETE /v1/php/{version}/extensions/{ext}", func(w http.ResponseWriter, r *http.Request) {
		t, err := s.Runtimes.RemoveExtension(r.PathValue("version"), r.PathValue("ext"))
		reply(w, 202, map[string]any{"data": t}, err)
	})
	m.HandleFunc("POST /v1/apache/install", func(w http.ResponseWriter, r *http.Request) {
		t, err := s.Sites.InstallApache()
		reply(w, 202, map[string]any{"data": t}, err)
	})
	m.HandleFunc("GET /v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		l, err := s.Tasks.List()
		reply(w, 200, map[string]any{"data": l}, err)
	})
	m.HandleFunc("GET /v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		t, err := s.Tasks.Get(r.PathValue("id"))
		reply(w, 200, map[string]any{"data": t}, err)
	})
	m.HandleFunc("GET /v1/sites/{domain}", func(w http.ResponseWriter, r *http.Request) {
		x, err := s.Sites.Get(r.PathValue("domain"))
		reply(w, 200, map[string]any{"data": x}, err)
	})
	m.HandleFunc("POST /v1/sites/{domain}/disable", func(w http.ResponseWriter, r *http.Request) {
		x, err := s.Sites.SetEnabled(r.PathValue("domain"), false)
		reply(w, 200, map[string]any{"data": x}, err)
	})
	m.HandleFunc("POST /v1/sites/{domain}/enable", func(w http.ResponseWriter, r *http.Request) {
		x, err := s.Sites.SetEnabled(r.PathValue("domain"), true)
		reply(w, 200, map[string]any{"data": x}, err)
	})
	m.HandleFunc("DELETE /v1/sites/{domain}", func(w http.ResponseWriter, r *http.Request) {
		err := s.Sites.Delete(r.PathValue("domain"))
		reply(w, 200, map[string]any{"data": nil}, err)
	})
	s.registerSiteTools(m)
	return m
}

func reply(w http.ResponseWriter, ok int, body any, err error) {
	switch {
	case err == nil:
		respond(w, ok, body)
	case errors.Is(err, sites.ErrInvalid):
		respond(w, 422, map[string]string{"error": err.Error()})
	case errors.Is(err, sitefiles.ErrInvalid):
		respond(w, 422, map[string]string{"error": err.Error()})
	case errors.Is(err, sitefiles.ErrTooLarge):
		respond(w, 413, map[string]string{"error": err.Error()})
	case errors.Is(err, sitefiles.ErrNotFound):
		respond(w, 404, map[string]string{"error": "not found"})
	case errors.Is(err, runtimes.ErrInvalid):
		respond(w, 422, map[string]string{"error": err.Error()})
	case errors.Is(err, runtimes.ErrInUse):
		respond(w, 409, map[string]string{"error": err.Error()})
	case errors.Is(err, sites.ErrExists):
		respond(w, 409, map[string]string{"error": err.Error()})
	case errors.Is(err, store.ErrNotFound):
		respond(w, 404, map[string]string{"error": "not found"})
	default:
		respond(w, 500, map[string]string{"error": err.Error()})
	}
}

func respond(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}
