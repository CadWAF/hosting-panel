package api

import (
	"mime"
	"net/http"
	"strconv"

	"hostingpanel/agent/internal/sitefiles"
)

const maxUpload = 512 << 20

// registerSiteTools adds SSH keys, cron, logs and the File Manager.
func (s *Server) registerSiteTools(m *http.ServeMux) {
	d := func(r *http.Request) string { return r.PathValue("domain") }
	id := func(r *http.Request) int64 { n, _ := strconv.ParseInt(r.PathValue("id"), 10, 64); return n }

	m.HandleFunc("GET /v1/sites/{domain}/ssh-keys", func(w http.ResponseWriter, r *http.Request) {
		k, err := s.Sites.ListKeys(d(r))
		reply(w, 200, map[string]any{"data": k}, err)
	})
	m.HandleFunc("POST /v1/sites/{domain}/ssh-keys", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Label     string `json:"label"`
			PublicKey string `json:"public_key"`
			Generate  bool   `json:"generate"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Generate {
			k, priv, err := s.Sites.GenerateKey(d(r), in.Label)
			reply(w, 201, map[string]any{"data": k, "private_key": priv}, err)
			return
		}
		k, err := s.Sites.AddKey(d(r), in.Label, in.PublicKey)
		reply(w, 201, map[string]any{"data": k}, err)
	})
	m.HandleFunc("DELETE /v1/sites/{domain}/ssh-keys/{id}", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"data": nil}, s.Sites.RemoveKey(d(r), id(r)))
	})

	m.HandleFunc("POST /v1/sites/{domain}/tls", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			WWW   bool   `json:"www"`
			Email string `json:"email"`
		}
		if !decode(w, r, &in) {
			return
		}
		t, err := s.Sites.IssueTLS(d(r), in.WWW, in.Email)
		reply(w, 202, map[string]any{"data": t}, err)
	})
	m.HandleFunc("DELETE /v1/sites/{domain}/tls", func(w http.ResponseWriter, r *http.Request) {
		x, err := s.Sites.RemoveTLS(d(r))
		reply(w, 200, map[string]any{"data": x}, err)
	})

	m.HandleFunc("GET /v1/sites/{domain}/cron", func(w http.ResponseWriter, r *http.Request) {
		j, err := s.Sites.ListCron(d(r))
		reply(w, 200, map[string]any{"data": j}, err)
	})
	m.HandleFunc("POST /v1/sites/{domain}/cron", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Schedule, Command string }
		if !decode(w, r, &in) {
			return
		}
		j, err := s.Sites.AddCron(d(r), in.Schedule, in.Command)
		reply(w, 201, map[string]any{"data": j}, err)
	})
	m.HandleFunc("DELETE /v1/sites/{domain}/cron/{id}", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"data": nil}, s.Sites.RemoveCron(d(r), id(r)))
	})

	m.HandleFunc("GET /v1/sites/{domain}/logs/{name}", func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("lines"))
		t, err := s.Sites.ReadLog(d(r), r.PathValue("name"), n)
		reply(w, 200, map[string]any{"data": t}, err)
	})

	// ---- File Manager ----
	withFS := func(w http.ResponseWriter, r *http.Request, fn func(f *sitefiles.FS) error) {
		f, err := s.Sites.Files(d(r))
		if err == nil {
			defer f.Close()
			err = fn(f)
		}
		reply(w, 200, map[string]any{"data": nil}, err)
	}
	q := func(r *http.Request) string { return r.URL.Query().Get("path") }

	m.HandleFunc("GET /v1/sites/{domain}/files", func(w http.ResponseWriter, r *http.Request) {
		f, err := s.Sites.Files(d(r))
		if err != nil {
			reply(w, 200, nil, err)
			return
		}
		defer f.Close()
		l, err := f.List(q(r))
		reply(w, 200, map[string]any{"data": l}, err)
	})
	m.HandleFunc("GET /v1/sites/{domain}/files/content", func(w http.ResponseWriter, r *http.Request) {
		f, err := s.Sites.Files(d(r))
		if err != nil {
			reply(w, 200, nil, err)
			return
		}
		defer f.Close()
		t, err := f.ReadText(q(r))
		reply(w, 200, map[string]any{"data": t}, err)
	})
	m.HandleFunc("PUT /v1/sites/{domain}/files/content", func(w http.ResponseWriter, r *http.Request) {
		withFS(w, r, func(f *sitefiles.FS) error { return f.Write(q(r), r.Body, sitefiles.MaxEditSize) })
	})
	m.HandleFunc("PUT /v1/sites/{domain}/files/upload", func(w http.ResponseWriter, r *http.Request) {
		withFS(w, r, func(f *sitefiles.FS) error { return f.Write(q(r), r.Body, maxUpload) })
	})
	m.HandleFunc("GET /v1/sites/{domain}/files/download", func(w http.ResponseWriter, r *http.Request) {
		f, err := s.Sites.Files(d(r))
		if err != nil {
			reply(w, 200, nil, err)
			return
		}
		defer f.Close()
		fh, st, err := f.OpenRead(q(r))
		if err != nil {
			reply(w, 200, nil, err)
			return
		}
		defer fh.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": st.Name()}))
		http.ServeContent(w, r, st.Name(), st.ModTime(), fh)
	})
	post := func(path string, fn func(f *sitefiles.FS, in map[string]any) error) {
		m.HandleFunc("POST /v1/sites/{domain}/files/"+path, func(w http.ResponseWriter, r *http.Request) {
			var in map[string]any
			if !decode(w, r, &in) {
				return
			}
			withFS(w, r, func(f *sitefiles.FS) error { return fn(f, in) })
		})
	}
	str := func(in map[string]any, k string) string { v, _ := in[k].(string); return v }
	post("mkdir", func(f *sitefiles.FS, in map[string]any) error { return f.Mkdir(str(in, "path")) })
	post("rename", func(f *sitefiles.FS, in map[string]any) error { return f.Rename(str(in, "from"), str(in, "to")) })
	post("trash", func(f *sitefiles.FS, in map[string]any) error { return f.Trash(str(in, "path")) })
	post("chmod", func(f *sitefiles.FS, in map[string]any) error {
		n, err := strconv.ParseUint(str(in, "mode"), 8, 32)
		if err != nil {
			return sitefiles.ErrInvalid
		}
		return f.Chmod(str(in, "path"), uint32(n))
	})
	post("extract", func(f *sitefiles.FS, in map[string]any) error {
		return f.Extract(str(in, "path"), str(in, "dest"), maxUpload*4)
	})
	m.HandleFunc("POST /v1/sites/{domain}/files/compress", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Paths []string }
		if !decode(w, r, &in) {
			return
		}
		f, err := s.Sites.Files(d(r))
		if err != nil {
			reply(w, 200, nil, err)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="download.zip"`)
		_ = f.Compress(in.Paths, w)
	})
}
