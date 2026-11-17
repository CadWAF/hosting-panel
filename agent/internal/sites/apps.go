package sites

import (
	"archive/tar"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/tasks"
)

// PMADownload is the official "latest release" URL; a test points it at a local server.
var PMADownload = "https://www.phpmyadmin.net/downloads/phpMyAdmin-latest-all-languages.tar.gz"

const pmaPlaceholder = "<?php echo 'Site "

type App struct {
	Domain      string    `json:"domain"`
	App         string    `json:"app"`
	Version     string    `json:"version"`
	InstalledAt time.Time `json:"installed_at"`
}

func (s *Service) initApps() error {
	_, err := s.st.DB().Exec(`CREATE TABLE IF NOT EXISTS site_apps(domain TEXT NOT NULL, app TEXT NOT NULL, version TEXT NOT NULL,
		installed_at TIMESTAMP NOT NULL, PRIMARY KEY(domain, app))`)
	return err
}

// Apps lists the applications the Panel installed into Sites.
func (s *Service) Apps() ([]App, error) {
	rows, err := s.st.DB().Query(`SELECT domain,app,version,installed_at FROM site_apps ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []App{}
	for rows.Next() {
		var a App
		if err := rows.Scan(&a.Domain, &a.App, &a.Version, &a.InstalledAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Service) docrootPath(x store.Site) string {
	return filepath.Join(s.homeOf(x), "public_html", x.Docroot)
}

func get(url string, limit int64) ([]byte, string, error) {
	c := &http.Client{Timeout: 10 * time.Minute}
	r, err := c.Get(url)
	if err != nil {
		return nil, "", err
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		return nil, "", fmt.Errorf("download %s: %s", url, r.Status)
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = fmt.Errorf("download %s is larger than %d MB", url, limit>>20)
	}
	return b, r.Request.URL.String(), err
}

// untar unpacks a .tar.gz into dest, dropping the first path component and refusing anything that would escape dest.
func untar(data []byte, dest string, maxBytes int64) error {
	gz, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	var total int64
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		for _, seg := range strings.Split(h.Name, "/") {
			if seg == ".." {
				return fmt.Errorf("unsafe path %q in archive", h.Name)
			}
		}
		parts := strings.SplitN(path.Clean(h.Name), "/", 2)
		if len(parts) < 2 || parts[1] == "." {
			continue
		}
		rel := path.Clean(parts[1])
		if strings.HasPrefix(rel, "..") || path.IsAbs(rel) {
			return fmt.Errorf("unsafe path %q in archive", h.Name)
		}
		target := filepath.Join(dest, filepath.FromSlash(rel))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			total += h.Size
			if total > maxBytes {
				return fmt.Errorf("archive is too large")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, io.LimitReader(tr, h.Size)); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
}

func randomSecret(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)[:n]
}

func pmaConfig(home string) string {
	return fmt.Sprintf(`<?php
/* managed by hostingpanel */
$cfg['blowfish_secret'] = '%s';
$i = 1;
$cfg['Servers'][$i]['auth_type'] = 'cookie';
$cfg['Servers'][$i]['host'] = 'localhost';
$cfg['Servers'][$i]['compress'] = false;
$cfg['Servers'][$i]['AllowNoPassword'] = false;
$cfg['Servers'][$i]['AllowRoot'] = false;
$cfg['TempDir'] = '%s/tmp/phpmyadmin';
$cfg['VersionCheck'] = false;
$cfg['SendErrorReports'] = 'never';
$cfg['LoginCookieValidity'] = 3600;
`, randomSecret(32), home)
}

// InstallPhpMyAdmin downloads the official release, verifies its checksum and unpacks it into the Site's web root.
func (s *Service) InstallPhpMyAdmin(domain string) (store.Task, error) {
	x, err := s.st.Get(domain)
	if err != nil {
		return store.Task{}, err
	}
	if x.Status != "enabled" {
		return store.Task{}, fmt.Errorf("%w: the site is disabled", ErrInvalid)
	}
	root := s.docrootPath(x)
	if ents, _ := os.ReadDir(root); len(ents) > 0 {
		ok := len(ents) == 1 && ents[0].Name() == "index.php"
		if ok {
			b, _ := os.ReadFile(filepath.Join(root, "index.php"))
			ok = strings.HasPrefix(string(b), pmaPlaceholder)
		}
		if !ok {
			return store.Task{}, fmt.Errorf("%w: the site's web root is not empty; use a new, empty site (for example pma.example.com)", ErrInvalid)
		}
	}
	return s.tasks.Start("phpmyadmin.install", "Install phpMyAdmin — "+domain, func(l *tasks.Log) error {
		l.Printf("Downloading phpMyAdmin")
		data, final, err := get(PMADownload, 100<<20)
		if err != nil {
			return err
		}
		sum, _, err := get(PMADownload+".sha256", 1<<10)
		if err != nil {
			return err
		}
		want, _, _ := strings.Cut(strings.TrimSpace(string(sum)), " ")
		got := sha256.Sum256(data)
		if hex.EncodeToString(got[:]) != strings.ToLower(want) {
			return fmt.Errorf("checksum mismatch: the download is not the official release")
		}
		l.Printf("Checksum verified (%s)", want[:12])
		_ = os.Remove(filepath.Join(root, "index.php"))
		if err := os.MkdirAll(root, 0o750); err != nil {
			return err
		}
		l.Printf("Unpacking")
		if err := untar(data, root, 400<<20); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(s.homeOf(x), "tmp", "phpmyadmin"), 0o750); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(root, "config.inc.php"), []byte(pmaConfig(s.homeOf(x))), 0o640); err != nil {
			return err
		}
		if err := s.run.RunW(l, "chown", "-R", x.User+":"+x.User, s.homeOf(x)); err != nil {
			return err
		}
		ver := "latest"
		if i := strings.Index(final, "phpMyAdmin-"); i >= 0 {
			ver, _, _ = strings.Cut(strings.TrimPrefix(final[i:], "phpMyAdmin-"), "-")
		}
		_, err = s.st.DB().Exec(`INSERT OR REPLACE INTO site_apps(domain,app,version,installed_at) VALUES(?,?,?,?)`, domain, "phpmyadmin", ver, s.now().UTC())
		l.Printf("phpMyAdmin %s is installed. Log in with a database user created in Databases (root login is disabled).", ver)
		return err
	})
}

// RemovePhpMyAdmin deletes phpMyAdmin's files and restores the placeholder page.
func (s *Service) RemovePhpMyAdmin(domain string) error {
	x, err := s.st.Get(domain)
	if err != nil {
		return err
	}
	var n int
	if err := s.st.DB().QueryRow(`SELECT COUNT(*) FROM site_apps WHERE domain=? AND app='phpmyadmin'`, domain).Scan(&n); err != nil || n == 0 {
		return ErrNotFound
	}
	root := s.docrootPath(x)
	ents, _ := os.ReadDir(root)
	for _, e := range ents {
		if err := os.RemoveAll(filepath.Join(root, e.Name())); err != nil {
			return err
		}
	}
	_ = os.RemoveAll(filepath.Join(s.homeOf(x), "tmp", "phpmyadmin"))
	_ = os.WriteFile(filepath.Join(root, "index.php"), []byte(pmaPlaceholder+domain+" is ready (PHP ' . PHP_VERSION . ')';\n"), 0o644)
	_ = s.run.Run("chown", "-R", x.User+":"+x.User, root)
	_, err = s.st.DB().Exec(`DELETE FROM site_apps WHERE domain=? AND app='phpmyadmin'`, domain)
	return err
}
