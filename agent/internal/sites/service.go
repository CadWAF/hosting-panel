// Package sites implements the Site lifecycle: Site User, PHP Pool, nginx vhost and optional Apache backend.
package sites

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"hostingpanel/agent/internal/sitefiles"
	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/system"
	"hostingpanel/agent/internal/tasks"
)

var (
	ErrInvalid  = errors.New("invalid input")
	ErrExists   = errors.New("site already exists")
	ErrNotFound = store.ErrNotFound

	domainRe  = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	versionRe = regexp.MustCompile(`^[0-9]\.[0-9]{1,2}$`)
)

// Paths are the host locations the agent writes to. Root is prefixed to all of
// them so a development machine can run the agent against a scratch directory.
type Paths struct {
	Root        string
	SitesDir    string // site homes
	NginxAvail  string
	NginxEn     string
	ApacheAvail string
	ApacheEn    string
	PHPDir      string // /etc/php
	RunDir      string // fpm sockets
	LogDir      string
	TrashDir    string
}

func DefaultPaths(root string) Paths {
	j := func(p string) string { return filepath.Join(root, p) }
	return Paths{root, j("/srv/sites"), j("/etc/nginx/sites-available"), j("/etc/nginx/sites-enabled"),
		j("/etc/apache2/sites-available"), j("/etc/apache2/sites-enabled"),
		j("/etc/php"), j("/run/php"), j("/var/log/hostingpanel/sites"), j("/srv/sites-trash")}
}

type Service struct {
	mu         sync.Mutex
	st         *store.Store
	run        system.Runner
	tasks      *tasks.Manager
	p          Paths
	defaultPHP string
	now        func() time.Time

	// Chown makes files created by the File Manager belong to the Site User; nil disables it (dry run).
	Chown func(user, path string)

	// RuntimeInstalled reports whether a PHP version is installed; nil means "assume yes".
	RuntimeInstalled func(string) bool
}

func New(st *store.Store, run system.Runner, t *tasks.Manager, p Paths, defaultPHP string) *Service {
	s := &Service{st: st, run: run, tasks: t, p: p, defaultPHP: defaultPHP, now: time.Now}
	if err := s.initAccess(); err != nil {
		panic(err)
	}
	return s
}

type storeSite = store.Site

func (s *Service) List() ([]store.Site, error)      { return s.st.List() }
func (s *Service) Get(d string) (store.Site, error) { return s.st.Get(d) }

func userFor(domain string) string {
	u := "s_" + strings.NewReplacer(".", "_", "-", "_").Replace(domain)
	if len(u) > 28 {
		u = u[:28]
	}
	return u
}

func (s *Service) data(x store.Site) tmplData {
	home := filepath.Join(s.p.SitesDir, x.User)
	ps, ok := presets[x.Preset]
	if !ok {
		ps = presets["medium"]
	}
	settings := map[string]string{"memory_limit": ps.memory}
	for k, v := range x.PHPSettings {
		settings[k] = v
	}
	return tmplData{
		Domain: x.Domain, User: x.User, PHPVersion: x.PHPVersion, Home: home,
		Docroot:    filepath.Join(home, "public_html", x.Docroot),
		Socket:     filepath.Join(s.p.RunDir, "php"+x.PHPVersion+"-fpm-"+x.User+".sock"),
		LogDir:     filepath.Join(s.p.LogDir, x.Domain),
		ApachePort: x.ApachePort, MaxChildren: ps.maxChildren, Terminate: ps.terminate,
		Settings: sortedKV(settings),
		TLS:      x.TLS != "",
		SSLNames: sslNames(x),
	}
}

func sslNames(x store.Site) string {
	if x.TLS == "www" {
		return x.Domain + " www." + x.Domain
	}
	return x.Domain
}

func (s *Service) poolPath(x store.Site) string {
	return filepath.Join(s.p.PHPDir, x.PHPVersion, "fpm", "pool.d", x.User+".conf")
}
func (s *Service) nginxAvail(x store.Site) string {
	return filepath.Join(s.p.NginxAvail, x.Domain+".conf")
}
func (s *Service) nginxEn(x store.Site) string { return filepath.Join(s.p.NginxEn, x.Domain+".conf") }
func (s *Service) apacheAvail(x store.Site) string {
	return filepath.Join(s.p.ApacheAvail, x.Domain+".conf")
}
func (s *Service) apacheEn(x store.Site) string { return filepath.Join(s.p.ApacheEn, x.Domain+".conf") }

func write(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func rm(paths ...string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

// apacheInstalled reports whether an Apache backend can be used.
func (s *Service) apacheInstalled() bool {
	_, err := os.Stat(s.p.ApacheAvail)
	return err == nil
}

// writeState writes every config file for x's desired state and removes stale ones from old.
func (s *Service) writeState(old *store.Site, x store.Site) error {
	d := s.data(x)
	if old != nil {
		if old.PHPVersion != x.PHPVersion {
			rm(s.poolPath(*old))
		}
		if old.WebMode == "apache" && (x.WebMode != "apache" || x.Status != "enabled") {
			rm(s.apacheEn(*old), s.apacheAvail(*old))
		}
	}
	if x.Status != "enabled" {
		rm(s.poolPath(x))
		v, err := render(disabledTmpl, d)
		if err != nil {
			return err
		}
		if err := write(s.nginxAvail(x), v); err != nil {
			return err
		}
		return relink(s.nginxAvail(x), s.nginxEn(x))
	}
	pool, err := render(poolTmpl, d)
	if err != nil {
		return err
	}
	if err := write(s.poolPath(x), pool); err != nil {
		return err
	}
	tmpl := nginxPHPTmpl
	if x.WebMode == "apache" {
		tmpl = nginxProxyTmpl
		av, err := render(apacheTmpl, d)
		if err != nil {
			return err
		}
		if err := write(s.apacheAvail(x), av); err != nil {
			return err
		}
		if err := relink(s.apacheAvail(x), s.apacheEn(x)); err != nil {
			return err
		}
	}
	v, err := render(tmpl, d)
	if err != nil {
		return err
	}
	if err := write(s.nginxAvail(x), v); err != nil {
		return err
	}
	return relink(s.nginxAvail(x), s.nginxEn(x))
}

func relink(avail, en string) error {
	_ = os.Remove(en)
	if err := os.MkdirAll(filepath.Dir(en), 0o755); err != nil {
		return err
	}
	return os.Symlink(avail, en)
}

// reload validates and reloads only the services touched by the change.
func (s *Service) reload(old *store.Site, x store.Site) error {
	if err := s.run.Run("nginx", "-t"); err != nil {
		return err
	}
	versions := map[string]bool{x.PHPVersion: true}
	apache := x.WebMode == "apache"
	if old != nil {
		versions[old.PHPVersion] = true
		apache = apache || old.WebMode == "apache"
	}
	for v := range versions {
		if err := s.run.Run("systemctl", "reload", "php"+v+"-fpm"); err != nil {
			return err
		}
	}
	if apache {
		if err := s.run.Run("apachectl", "configtest"); err != nil {
			return err
		}
		if err := s.run.Run("systemctl", "reload", "apache2"); err != nil {
			return err
		}
	}
	return s.run.Run("systemctl", "reload", "nginx")
}

func (s *Service) validatePHP(v string) error {
	if !versionRe.MatchString(v) {
		return fmt.Errorf("%w: php_version", ErrInvalid)
	}
	if s.RuntimeInstalled != nil && !s.RuntimeInstalled(v) {
		return fmt.Errorf("%w: PHP %s is not installed, install it first", ErrInvalid, v)
	}
	return nil
}

func (s *Service) validateMode(mode string) error {
	switch mode {
	case "nginx":
		return nil
	case "apache":
		if !s.apacheInstalled() {
			return fmt.Errorf("%w: Apache is not installed, install it first", ErrInvalid)
		}
		return nil
	}
	return fmt.Errorf("%w: web_mode", ErrInvalid)
}

func (s *Service) allocPort() (int, error) {
	used, err := s.st.UsedApachePorts()
	if err != nil {
		return 0, err
	}
	for p := 8100; p < 8999; p++ {
		if !used[p] {
			return p, nil
		}
	}
	return 0, errors.New("no free apache port")
}

type CreateInput struct {
	Domain     string `json:"domain"`
	PHPVersion string `json:"php_version"`
	WebMode    string `json:"web_mode"`
	Preset     string `json:"preset"`
}

// Create provisions a Site. On failure everything done so far is undone.
func (s *Service) Create(in CreateInput) (store.Site, error) {
	domain := strings.ToLower(strings.TrimSpace(in.Domain))
	if !domainRe.MatchString(domain) || len(domain) > 253 {
		return store.Site{}, fmt.Errorf("%w: domain", ErrInvalid)
	}
	if in.PHPVersion == "" {
		in.PHPVersion = s.defaultPHP
	}
	if in.WebMode == "" {
		in.WebMode = "nginx"
	}
	if in.Preset == "" {
		in.Preset = "medium"
	}
	if _, ok := presets[in.Preset]; !ok {
		return store.Site{}, fmt.Errorf("%w: preset", ErrInvalid)
	}
	if err := s.validatePHP(in.PHPVersion); err != nil {
		return store.Site{}, err
	}
	if err := s.validateMode(in.WebMode); err != nil {
		return store.Site{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.st.Get(domain); err == nil {
		return store.Site{}, ErrExists
	}
	x := store.Site{Domain: domain, User: userFor(domain), PHPVersion: in.PHPVersion, Status: "enabled",
		WebMode: in.WebMode, Preset: in.Preset, PHPSettings: map[string]string{}, CreatedAt: s.now().UTC()}
	if taken, err := s.st.UserTaken(x.User); err != nil {
		return x, err
	} else if taken {
		return x, fmt.Errorf("%w: user name collision, use a shorter domain", ErrInvalid)
	}
	if x.WebMode == "apache" {
		p, err := s.allocPort()
		if err != nil {
			return x, err
		}
		x.ApachePort = p
	}
	d := s.data(x)
	undo := []func(){}
	fail := func(err error) (store.Site, error) {
		for i := len(undo) - 1; i >= 0; i-- {
			undo[i]()
		}
		return store.Site{}, err
	}

	if err := s.run.Run("useradd", "--system", "--create-home", "--home-dir", d.Home, "--shell", "/bin/bash", "--user-group", "--groups", SSHGroup, x.User); err != nil {
		return fail(err)
	}
	undo = append(undo, func() { s.removeUser(x.User) })
	for _, dir := range []string{d.Docroot, filepath.Join(d.Home, "tmp"), d.LogDir} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fail(err)
		}
	}
	_ = s.run.Run("usermod", "-aG", x.User, "www-data")
	if err := write(filepath.Join(d.Docroot, "index.php"),
		"<?php echo 'Site "+domain+" is ready (PHP ' . PHP_VERSION . ')';\n"); err != nil {
		return fail(err)
	}
	_ = s.run.Run("chown", "-R", x.User+":"+x.User, d.Home) // after every file is written
	undo = append(undo, func() {
		rm(s.poolPath(x), s.nginxEn(x), s.nginxAvail(x), s.apacheEn(x), s.apacheAvail(x))
	})
	if err := s.writeState(nil, x); err != nil {
		return fail(err)
	}
	if err := s.reload(nil, x); err != nil {
		return fail(err)
	}
	if err := s.st.Insert(x); err != nil {
		return fail(err)
	}
	return x, nil
}

// UpdateInput holds optional changes; nil fields stay as they are.
type UpdateInput struct {
	PHPVersion  *string           `json:"php_version"`
	WebMode     *string           `json:"web_mode"`
	Preset      *string           `json:"preset"`
	PHPSettings map[string]string `json:"php_settings"`
	Docroot     *string           `json:"docroot"`
}

func (s *Service) Update(domain string, in UpdateInput) (store.Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.st.Get(domain)
	if err != nil {
		return old, err
	}
	x := old
	if in.PHPVersion != nil && *in.PHPVersion != old.PHPVersion {
		if err := s.validatePHP(*in.PHPVersion); err != nil {
			return old, err
		}
		x.PHPVersion = *in.PHPVersion
	}
	if in.WebMode != nil && *in.WebMode != old.WebMode {
		if err := s.validateMode(*in.WebMode); err != nil {
			return old, err
		}
		x.WebMode = *in.WebMode
		if x.WebMode == "apache" && x.ApachePort == 0 {
			p, err := s.allocPort()
			if err != nil {
				return old, err
			}
			x.ApachePort = p
		}
	}
	if in.Preset != nil {
		if _, ok := presets[*in.Preset]; !ok {
			return old, fmt.Errorf("%w: preset", ErrInvalid)
		}
		x.Preset = *in.Preset
	}
	if in.PHPSettings != nil {
		if err := validateSettings(in.PHPSettings); err != nil {
			return old, err
		}
		x.PHPSettings = in.PHPSettings
	}
	if in.Docroot != nil {
		d, err := cleanDocroot(*in.Docroot)
		if err != nil {
			return old, err
		}
		x.Docroot = d
	}
	return s.commit(&old, x)
}

// commit writes, reloads and persists x; on failure it restores old.
func (s *Service) commit(old *store.Site, x store.Site) (store.Site, error) {
	restore := func() {
		_ = s.writeState(&x, *old)
		_ = s.reload(&x, *old)
	}
	if err := s.writeState(old, x); err != nil {
		restore()
		return *old, err
	}
	if err := s.reload(old, x); err != nil {
		restore()
		return *old, err
	}
	if err := s.st.Update(x); err != nil {
		restore()
		return *old, err
	}
	if old.Status != x.Status {
		_ = s.installCron(x) // pause/resume scheduled tasks with the Site
	}
	return x, nil
}

func (s *Service) SetEnabled(domain string, enabled bool) (store.Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.st.Get(domain)
	if err != nil {
		return old, err
	}
	want := "disabled"
	if enabled {
		want = "enabled"
	}
	if old.Status == want {
		return old, nil
	}
	x := old
	x.Status = want
	return s.commit(&old, x)
}

// Delete removes the Site's configuration and user. The home directory is moved
// to the trash directory rather than erased; the Administrator removes it later.
func (s *Service) Delete(domain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, err := s.st.Get(domain)
	if err != nil {
		return err
	}
	rm(s.poolPath(x), s.nginxEn(x), s.nginxAvail(x), s.apacheEn(x), s.apacheAvail(x))
	if err := s.reload(&x, x); err != nil {
		return err
	}
	_ = s.run.Run("crontab", "-r", "-u", x.User)
	if err := s.removeUser(x.User); err != nil {
		return err
	}
	if err := os.MkdirAll(s.p.TrashDir, 0o755); err != nil {
		return err
	}
	home := filepath.Join(s.p.SitesDir, x.User)
	dst := filepath.Join(s.p.TrashDir, fmt.Sprintf("%s-%d", x.User, s.now().UnixNano()))
	if err := os.Rename(home, dst); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, _ = s.st.DB().Exec(`DELETE FROM ssh_keys WHERE domain=?`, domain)
	_, _ = s.st.DB().Exec(`DELETE FROM cron_jobs WHERE domain=?`, domain)
	return s.st.Delete(domain)
}

// removeUser deletes a Site User and its group. www-data is a member of the group (so nginx can
// read the docroot), which makes userdel keep the group behind; drop the membership first.
func (s *Service) removeUser(user string) error {
	_ = s.run.Run("gpasswd", "-d", "www-data", user)
	var err error
	if s.run.Run("id", "-u", user) == nil { // already gone after an interrupted delete: nothing to do
		err = s.run.Run("userdel", user)
	}
	_ = s.run.Run("groupdel", user) // fails harmlessly when userdel already removed it
	return err
}

// InstallApache installs Apache as a local-only backend (never on public ports).
func (s *Service) InstallApache() (store.Task, error) {
	return s.tasks.Start("apache.install", "Install Apache backend", func(l *tasks.Log) error {
		if err := s.run.RunW(l, "apt-get", "-y", "install", "apache2"); err != nil {
			return err
		}
		for _, a := range [][]string{{"a2dismod", "-f", "mpm_prefork"}, {"a2enmod", "mpm_event", "proxy_fcgi", "setenvif", "rewrite", "headers"}} {
			if err := s.run.RunW(l, a[0], a[1:]...); err != nil {
				return err
			}
		}
		// Apache must not take ports 80/443: nginx owns them. Each site vhost declares its own local Listen.
		if err := write(filepath.Join(s.p.Root, "/etc/apache2/ports.conf"), "# managed by hostingpanel: Apache listens only on local ports (never 80/443).\n# Apache refuses to start without any Listen, so keep an unused loopback placeholder; each site adds its own.\nListen 127.0.0.1:8099\n"); err != nil {
			return err
		}
		if err := s.run.RunW(l, "a2dissite", "000-default"); err != nil {
			return err
		}
		if err := s.run.RunW(l, "systemctl", "enable", "apache2"); err != nil {
			return err
		}
		if err := s.run.RunW(l, "systemctl", "restart", "apache2"); err != nil {
			_ = s.run.RunW(l, "journalctl", "-u", "apache2", "-n", "15", "--no-pager")
			return err
		}
		return nil
	})
}

// Files opens the File Manager jail for a Site.
func (s *Service) Files(domain string) (*sitefiles.FS, error) {
	x, err := s.st.Get(domain)
	if err != nil {
		return nil, err
	}
	home := filepath.Join(s.p.SitesDir, x.User)
	return sitefiles.Open(home, func(rel string) {
		if s.Chown != nil {
			s.Chown(x.User, filepath.Join(home, rel))
		}
	})
}

var docrootRe = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*$`)

// cleanDocroot accepts "" or a plain relative folder such as "public"; never anything that could leave public_html.
func cleanDocroot(d string) (string, error) {
	d = strings.Trim(strings.TrimSpace(d), "/")
	if d == "" {
		return "", nil
	}
	for _, part := range strings.Split(d, "/") {
		if part == "." || part == ".." {
			return "", fmt.Errorf("%w: document root", ErrInvalid)
		}
	}
	if !docrootRe.MatchString(d) {
		return "", fmt.Errorf("%w: document root", ErrInvalid)
	}
	return d, nil
}
