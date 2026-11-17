package sites

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hostingpanel/agent/internal/store"
)

func vhostOf(t *testing.T, p Paths, d string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(p.NginxAvail, d+".conf"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWebSettingsRenderProxyRedirectGzipAndAuth(t *testing.T) {
	s, _, p := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "app.example.com"}); err != nil {
		t.Fatal(err)
	}
	mb, gz, sc := 512, true, true
	user := "admin"
	px := []store.Proxy{{Path: "/", Upstream: "http://127.0.0.1:8080", WebSocket: true}}
	rd := []store.Redirect{{From: "/old", To: "https://example.org/new", Code: 301}, {From: "/blog/", To: "/news/", Code: 302}}
	if _, err := s.Update("app.example.com", UpdateInput{Web: &WebInput{MaxBodyMB: &mb, Gzip: &gz, StaticCache: &sc, Proxies: &px, Redirects: &rd, AuthUser: &user, AuthPassword: "s3cretpass"}}); err != nil {
		t.Fatal(err)
	}
	v := vhostOf(t, p, "app.example.com")
	for _, want := range []string{"client_max_body_size 512m;", "gzip on;", "proxy_pass http://127.0.0.1:8080;", "$connection_upgrade",
		"location = /old {", "return 301 https://example.org/new;", "location ^~ /blog/ {", "auth_basic_user_file", "auth_basic off;"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q in\n%s", want, v)
		}
	}
	if strings.Contains(v, "fastcgi_pass") || strings.Contains(v, "try_files") {
		t.Errorf("php locations must be gone when / is proxied:\n%s", v)
	}
	h, _ := os.ReadFile(s.authFile("app.example.com"))
	if !strings.HasPrefix(string(h), "admin:$2") || strings.Contains(string(h), "s3cretpass") {
		t.Errorf("htpasswd: %s", h)
	}
	if _, err := os.Stat(filepath.Join(p.Root, "etc/nginx/conf.d/hostingpanel-upgrade.conf")); err != nil {
		t.Error("websocket map missing")
	}
	// Turning auth off drops the directive.
	off := ""
	if _, err := s.Update("app.example.com", UpdateInput{Web: &WebInput{AuthUser: &off}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(vhostOf(t, p, "app.example.com"), "auth_basic_user_file") {
		t.Error("auth still rendered")
	}
}

func TestWebSettingsRejectInjection(t *testing.T) {
	s, _, _ := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "x.example.com"}); err != nil {
		t.Fatal(err)
	}
	bad := []WebInput{
		{Proxies: &[]store.Proxy{{Path: "/", Upstream: "http://a.com; return 200"}}},
		{Proxies: &[]store.Proxy{{Path: "/a b", Upstream: "http://127.0.0.1:1"}}},
		{Proxies: &[]store.Proxy{{Path: "/../x", Upstream: "http://127.0.0.1:1"}}},
		{Proxies: &[]store.Proxy{{Path: "/a", Upstream: "file:///etc/passwd"}}},
		{Redirects: &[]store.Redirect{{From: "/a", To: "http://x.com/\n}"}}},
		{Redirects: &[]store.Redirect{{From: "/a", To: "/b;x", Code: 301}}},
		{Redirects: &[]store.Redirect{{From: "/a", To: "/b", Code: 308}}},
		{AuthUser: ptr("a b")},
	}
	for i, w := range bad {
		w := w
		if _, err := s.Update("x.example.com", UpdateInput{Web: &w}); !errors.Is(err, ErrInvalid) {
			t.Errorf("case %d accepted: %v", i, err)
		}
	}
	u := "bob" // a user without a password is refused
	if _, err := s.Update("x.example.com", UpdateInput{Web: &WebInput{AuthUser: &u}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("auth without password accepted: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

func TestComposerNeedsEnableAndBuildsSafeCommand(t *testing.T) {
	s, r, p := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "c.example.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RunComposer("c.example.com", ComposerInput{Command: "install"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("composer must be opt-in: %v", err)
	}
	if _, err := s.Update("c.example.com", UpdateInput{Composer: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(p.SitesDir, "s_c_example_com")
	for _, bad := range []ComposerInput{{Command: "exec"}, {Command: "require", Package: "x; rm -rf /"}, {Command: "require", Package: "nope"},
		{Command: "install", Dir: "../other"}, {Command: "install", Dir: "public_html"} /* no composer.json yet */} {
		if _, err := s.RunComposer("c.example.com", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v accepted: %v", bad, err)
		}
	}
	_ = os.WriteFile(filepath.Join(home, "public_html", "composer.json"), []byte("{}"), 0o644)
	tk, err := s.RunComposer("c.example.com", ComposerInput{Command: "require", Package: "monolog/monolog:^3.0"})
	if err != nil {
		t.Fatal(err)
	}
	s.tasks.Wait()
	_ = tk
	j := strings.Join(r.Calls, "\n")
	if !strings.Contains(j, "runuser -u s_c_example_com -- php8.3 /usr/bin/composer require --prefer-dist --no-progress monolog/monolog:^3.0 --no-interaction --no-ansi") {
		t.Errorf("command not as expected:\n%s", j)
	}
	st, _ := s.Composer("c.example.com")
	if !st.Enabled || len(st.Projects) != 1 || st.Projects[0] != "public_html" {
		t.Errorf("state %+v", st)
	}
}

func fakePMA(t *testing.T, good bool) string {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"phpMyAdmin-5.2.3-all-languages/index.php": "<?php //pma", "phpMyAdmin-5.2.3-all-languages/libraries/a.php": "x"} {
		_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	if !good {
		_ = tw.WriteHeader(&tar.Header{Name: "phpMyAdmin-5.2.3-all-languages/../../evil", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte("x"))
	}
	tw.Close()
	gz.Close()
	sum := sha256.Sum256(buf.Bytes())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			w.Write([]byte(hex.EncodeToString(sum[:]) + "  phpMyAdmin.tar.gz\n"))
			return
		}
		w.Write(buf.Bytes())
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/phpMyAdmin-5.2.3-all-languages.tar.gz"
}

func TestInstallAndRemovePhpMyAdmin(t *testing.T) {
	s, _, p := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "pma.example.com"}); err != nil {
		t.Fatal(err)
	}
	old := PMADownload
	defer func() { PMADownload = old }()
	PMADownload = fakePMA(t, true)
	if _, err := s.InstallPhpMyAdmin("pma.example.com"); err != nil {
		t.Fatal(err)
	}
	s.tasks.Wait()
	root := filepath.Join(p.SitesDir, "s_pma_example_com", "public_html")
	for _, f := range []string{"index.php", "libraries/a.php", "config.inc.php"} {
		if _, err := os.Stat(filepath.Join(root, f)); err != nil {
			t.Errorf("%s missing", f)
		}
	}
	cfg, _ := os.ReadFile(filepath.Join(root, "config.inc.php"))
	if !strings.Contains(string(cfg), "AllowRoot'] = false") {
		t.Errorf("root login must be off")
	}
	apps, _ := s.Apps()
	if len(apps) != 1 || apps[0].Version != "5.2.3" {
		t.Fatalf("apps %+v", apps)
	}
	if err := s.RemovePhpMyAdmin("pma.example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "libraries")); err == nil {
		t.Error("files not removed")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "index.php")); !strings.HasPrefix(string(b), pmaPlaceholder) {
		t.Error("placeholder not restored")
	}
}

func TestPhpMyAdminRefusesNonEmptySiteAndBadArchive(t *testing.T) {
	s, _, p := newSvc(t)
	_, _ = s.Create(CreateInput{Domain: "w.example.com"})
	root := filepath.Join(p.SitesDir, "s_w_example_com", "public_html")
	_ = os.WriteFile(filepath.Join(root, "wp-config.php"), []byte("x"), 0o644)
	if _, err := s.InstallPhpMyAdmin("w.example.com"); !errors.Is(err, ErrInvalid) {
		t.Errorf("non-empty site accepted: %v", err)
	}
	_, _ = s.Create(CreateInput{Domain: "e.example.com"})
	old := PMADownload
	defer func() { PMADownload = old }()
	PMADownload = fakePMA(t, false)
	_, _ = s.InstallPhpMyAdmin("e.example.com")
	s.tasks.Wait()
	if apps, _ := s.Apps(); len(apps) != 0 {
		t.Errorf("unsafe archive must not install: %+v", apps)
	}
}
