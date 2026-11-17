package sites

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/system"
	"hostingpanel/agent/internal/tasks"
)

func newSvc(t *testing.T) (*Service, *system.DryRunner, Paths) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := &system.DryRunner{}
	p := DefaultPaths(filepath.Join(dir, "root"))
	return New(st, r, tasks.New(st), p, "8.3"), r, p
}

func TestCreateWritesConfigAndRuns(t *testing.T) {
	s, r, p := newSvc(t)
	x, err := s.Create(CreateInput{Domain: "Example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if x.Domain != "example.com" || x.User != "s_example_com" || x.PHPVersion != "8.3" {
		t.Fatalf("bad site %+v", x)
	}
	pool, _ := os.ReadFile(filepath.Join(p.PHPDir, "8.3/fpm/pool.d/s_example_com.conf"))
	if !strings.Contains(string(pool), "user = s_example_com") || !strings.Contains(string(pool), "open_basedir") {
		t.Fatalf("pool: %s", pool)
	}
	if _, err := os.Lstat(filepath.Join(p.NginxEn, "example.com.conf")); err != nil {
		t.Fatal("vhost not enabled")
	}
	joined := strings.Join(r.Calls, "\n")
	for _, want := range []string{"useradd", "nginx -t", "systemctl reload php8.3-fpm", "systemctl reload nginx"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing call %q in\n%s", want, joined)
		}
	}
}

func TestCreateValidationAndDuplicate(t *testing.T) {
	s, _, _ := newSvc(t)
	for _, bad := range []string{"", "nodot", "a b.com", "../x.com", "x.com;rm", "-a.com"} {
		if _, err := s.Create(CreateInput{Domain: bad}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q: want ErrInvalid, got %v", bad, err)
		}
	}
	if _, err := s.Create(CreateInput{Domain: "ok.com", PHPVersion: "8.x"}); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad version accepted: %v", err)
	}
	if _, err := s.Create(CreateInput{Domain: "ok.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(CreateInput{Domain: "ok.com"}); !errors.Is(err, ErrExists) {
		t.Errorf("want ErrExists, got %v", err)
	}
}

func TestDisableEnableDelete(t *testing.T) {
	s, _, p := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "a.example.org", PHPVersion: "8.2"}); err != nil {
		t.Fatal(err)
	}
	pool := filepath.Join(p.PHPDir, "8.2/fpm/pool.d/s_a_example_org.conf")
	if _, err := s.SetEnabled("a.example.org", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pool); !os.IsNotExist(err) {
		t.Error("pool should be removed when disabled")
	}
	v, _ := os.ReadFile(filepath.Join(p.NginxAvail, "a.example.org.conf"))
	if !strings.Contains(string(v), "return 503") {
		t.Error("disabled vhost should return 503")
	}
	if _, err := s.SetEnabled("a.example.org", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pool); err != nil {
		t.Error("pool should be back")
	}
	if err := s.Delete("a.example.org"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("a.example.org"); !errors.Is(err, ErrNotFound) {
		t.Error("site should be gone")
	}
	trash, _ := os.ReadDir(p.TrashDir)
	if len(trash) != 1 {
		t.Errorf("home should be kept in trash, got %d", len(trash))
	}
	if _, err := s.SetEnabled("nope.com", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("want not found, got %v", err)
	}
}

func TestUpdateMovesPoolBetweenPHPVersionsAndWritesSettings(t *testing.T) {
	s, _, p := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "m.example.com", PHPVersion: "8.1"}); err != nil {
		t.Fatal(err)
	}
	v, pre := "8.4", "large"
	x, err := s.Update("m.example.com", UpdateInput{PHPVersion: &v, Preset: &pre,
		PHPSettings: map[string]string{"memory_limit": "1G", "upload_max_filesize": "64M"}})
	if err != nil {
		t.Fatal(err)
	}
	if x.PHPVersion != "8.4" {
		t.Fatalf("%+v", x)
	}
	if _, err := os.Stat(filepath.Join(p.PHPDir, "8.1/fpm/pool.d/s_m_example_com.conf")); !os.IsNotExist(err) {
		t.Error("old pool should be removed")
	}
	b, _ := os.ReadFile(filepath.Join(p.PHPDir, "8.4/fpm/pool.d/s_m_example_com.conf"))
	for _, want := range []string{"memory_limit] = 1G", "upload_max_filesize] = 64M", "pm.max_children = 24"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("pool missing %q:\n%s", want, b)
		}
	}
}

func TestSettingsRejectUnknownAndInjection(t *testing.T) {
	s, _, _ := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "i.example.com"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []map[string]string{
		{"auto_prepend_file": "/x"},
		{"memory_limit": "1G\nphp_admin_value[disable_functions] = "},
		{"date.timezone": "UTC; evil"},
	} {
		if _, err := s.Update("i.example.com", UpdateInput{PHPSettings: bad}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%v: want ErrInvalid, got %v", bad, err)
		}
	}
}

func TestApacheModeNeedsApacheAndProxies(t *testing.T) {
	s, r, p := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "ap.example.com", WebMode: "apache"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("apache not installed must be rejected, got %v", err)
	}
	_ = os.MkdirAll(p.ApacheAvail, 0o755)
	x, err := s.Create(CreateInput{Domain: "ap.example.com", WebMode: "apache"})
	if err != nil {
		t.Fatal(err)
	}
	if x.ApachePort != 8100 {
		t.Errorf("port %d", x.ApachePort)
	}
	ng, _ := os.ReadFile(filepath.Join(p.NginxAvail, "ap.example.com.conf"))
	if !strings.Contains(string(ng), "proxy_pass http://127.0.0.1:8100") {
		t.Errorf("nginx: %s", ng)
	}
	ap, _ := os.ReadFile(filepath.Join(p.ApacheAvail, "ap.example.com.conf"))
	if !strings.Contains(string(ap), "Listen 127.0.0.1:8100") || !strings.Contains(string(ap), "proxy:unix:") {
		t.Errorf("apache: %s", ap)
	}
	if !strings.Contains(strings.Join(r.Calls, "\n"), "apachectl configtest") {
		t.Error("apache config must be tested")
	}
	nginx := "nginx"
	if _, err := s.Update("ap.example.com", UpdateInput{WebMode: &nginx}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.ApacheAvail, "ap.example.com.conf")); !os.IsNotExist(err) {
		t.Error("apache vhost should be removed when switching to nginx")
	}
}

const goodKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl user@laptop"

func TestPasteExistingKeyWritesAuthorizedKeys(t *testing.T) {
	s, r, p := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "k.example.com"}); err != nil {
		t.Fatal(err)
	}
	k, err := s.AddKey("k.example.com", "", goodKey)
	if err != nil {
		t.Fatal(err)
	}
	if k.Label != "user@laptop" || !strings.HasPrefix(k.Fingerprint, "SHA256:") {
		t.Fatalf("%+v", k)
	}
	b, _ := os.ReadFile(filepath.Join(p.SitesDir, "s_k_example_com", ".ssh", "authorized_keys"))
	if strings.TrimSpace(string(b)) != strings.Join(strings.Fields(goodKey)[:2], " ") {
		t.Fatalf("authorized_keys: %q", b)
	}
	if _, err := s.AddKey("k.example.com", "again", goodKey); !errors.Is(err, ErrExists) {
		t.Fatalf("duplicate: %v", err)
	}
	if !strings.Contains(strings.Join(r.Calls, "\n"), "--groups "+SSHGroup) {
		t.Error("site user must join the key-only ssh group")
	}
	if err := s.RemoveKey("k.example.com", k.ID); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(filepath.Join(p.SitesDir, "s_k_example_com", ".ssh", "authorized_keys"))
	if len(strings.TrimSpace(string(b))) != 0 {
		t.Fatalf("key must be removed from file: %q", b)
	}
}

func TestKeyValidationRejectsOptionsMultilineAndGarbage(t *testing.T) {
	s, _, _ := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "v.example.com"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "garbage", `command="rm -rf /" ` + goodKey, goodKey + "\n" + goodKey, "ssh-dss AAAAB3NzaC1kc3M="} {
		if _, err := s.AddKey("v.example.com", "x", bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q accepted: %v", bad, err)
		}
	}
}

func TestGeneratedKeyReturnsPrivateOnceAndStoresOnlyPublic(t *testing.T) {
	s, _, _ := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "g.example.com"}); err != nil {
		t.Fatal(err)
	}
	k, priv, err := s.GenerateKey("g.example.com", "generated")
	if err != nil || !strings.Contains(priv, "BEGIN OPENSSH PRIVATE KEY") {
		t.Fatalf("%v %q", err, priv)
	}
	l, _ := s.ListKeys("g.example.com")
	if len(l) != 1 || strings.Contains(l[0].PublicKey, "PRIVATE") || l[0].ID != k.ID {
		t.Fatalf("%+v", l)
	}
}

func TestCronValidationAndPauseOnDisable(t *testing.T) {
	s, r, _ := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "c.example.com"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][2]string{{"* * * *", "x"}, {"@daily", ""}, {"@daily", "a\nb"}, {"; rm", "x"}} {
		if _, err := s.AddCron("c.example.com", bad[0], bad[1]); !errors.Is(err, ErrInvalid) {
			t.Errorf("%v accepted: %v", bad, err)
		}
	}
	if _, err := s.AddCron("c.example.com", "*/5 * * * *", "php /srv/x/artisan schedule:run"); err != nil {
		t.Fatal(err)
	}
	if got := r.Stdin[len(r.Stdin)-1]; !strings.Contains(got, "*/5 * * * * php") {
		t.Fatalf("crontab: %q", got)
	}
	if _, err := s.SetEnabled("c.example.com", false); err != nil {
		t.Fatal(err)
	}
	if got := r.Stdin[len(r.Stdin)-1]; strings.Contains(got, "artisan") {
		t.Fatalf("disabled site must have an empty crontab, got %q", got)
	}
	if _, err := s.SetEnabled("c.example.com", true); err != nil {
		t.Fatal(err)
	}
	if got := r.Stdin[len(r.Stdin)-1]; !strings.Contains(got, "artisan") {
		t.Fatalf("jobs must resume: %q", got)
	}
}

func TestReadLogTailsAndRejectsUnknownName(t *testing.T) {
	s, _, p := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "l.example.com"}); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i := 1; i <= 500; i++ {
		b.WriteString("line" + strconv.Itoa(i) + "\n")
	}
	_ = os.WriteFile(filepath.Join(p.LogDir, "l.example.com", "error.log"), []byte(b.String()), 0o644)
	out, err := s.ReadLog("l.example.com", "error", 3)
	if err != nil || out != "line498\nline499\nline500" {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := s.ReadLog("l.example.com", "../../etc/passwd", 3); !errors.Is(err, ErrInvalid) {
		t.Fatalf("traversal: %v", err)
	}
	if out, _ := s.ReadLog("l.example.com", "access", 5); out != "" {
		t.Fatal("missing log should be empty")
	}
}

func TestFilesAreJailedToTheSiteHome(t *testing.T) {
	s, _, _ := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "f.example.com"}); err != nil {
		t.Fatal(err)
	}
	f, err := s.Files("f.example.com")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	es, err := f.List("public_html")
	if err != nil || len(es) != 1 || es[0].Name != "index.php" {
		t.Fatalf("%+v %v", es, err)
	}
	if _, err := s.Files("missing.example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestTLSIssueSwitchesVhostToHTTPSAndBack(t *testing.T) {
	s, r, p := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "tls.example"}); err != nil {
		t.Fatal(err)
	}
	vh := filepath.Join(p.NginxAvail, "tls.example.conf")
	b, _ := os.ReadFile(vh)
	if !strings.Contains(string(b), "acme-challenge") || strings.Contains(string(b), "443") {
		t.Fatalf("plain vhost wrong:\n%s", b)
	}
	if _, err := s.IssueTLS("tls.example", false, "not an email"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
	task, err := s.IssueTLS("tls.example", true, "me@example.com")
	if err != nil {
		t.Fatal(err)
	}
	s.tasks.Wait()
	if done, _ := s.tasks.Get(task.ID); done.Status != "done" {
		t.Fatalf("task %+v", done)
	}
	b, _ = os.ReadFile(vh)
	for _, want := range []string{"listen 443 ssl", "return 301 https://", "/etc/letsencrypt/live/tls.example/fullchain.pem", "server_name tls.example www.tls.example;"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %q in\n%s", want, b)
		}
	}
	if !strings.Contains(strings.Join(r.Calls, "\n"), "certbot certonly --webroot") {
		t.Errorf("certbot not run: %v", r.Calls)
	}
	if _, err := s.RemoveTLS("tls.example"); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(vh)
	if strings.Contains(string(b), "443") {
		t.Fatalf("still https:\n%s", b)
	}
}

func TestDocrootServesASubfolderAndRejectsEscapes(t *testing.T) {
	s, _, p := newSvc(t)
	if _, err := s.Create(CreateInput{Domain: "app.example.com"}); err != nil {
		t.Fatal(err)
	}
	pub := "public"
	if _, err := s.Update("app.example.com", UpdateInput{Docroot: &pub}); err != nil {
		t.Fatal(err)
	}
	conf, _ := os.ReadFile(filepath.Join(p.NginxAvail, "app.example.com.conf"))
	if !strings.Contains(string(conf), "/s_app_example_com/public_html/public;") {
		t.Fatalf("root not applied:\n%s", conf)
	}
	for _, bad := range []string{"../etc", "a/../../b", "/abs/../x", "a b", "x;y", "."} {
		b := bad
		if _, err := s.Update("app.example.com", UpdateInput{Docroot: &b}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q should be rejected, got %v", bad, err)
		}
	}
	empty := ""
	if _, err := s.Update("app.example.com", UpdateInput{Docroot: &empty}); err != nil {
		t.Fatal(err)
	}
}
