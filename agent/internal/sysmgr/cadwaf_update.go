package sysmgr

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/tasks"
)

// CWReleaseURL is the "latest release" endpoint of the public CadWAF Firewall repository (a test points it elsewhere).
var CWReleaseURL = "https://api.github.com/repos/CadWAF/firewall/releases/latest"

const (
	cwMaxArchive = 64 << 20
	cwMaxBinary  = 96 << 20
)

// CWRelease describes the newest published version next to the installed one.
type CWRelease struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"` // a newer version can be installed
	Notes     string `json:"notes"`
	URL       string `json:"url"`
	Error     string `json:"error,omitempty"`
}

type cwAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

type cwRel struct {
	Tag    string    `json:"tag_name"`
	Body   string    `json:"body"`
	URL    string    `json:"html_url"`
	Assets []cwAsset `json:"assets"`
}

var cwCache struct {
	sync.Mutex
	at  time.Time
	rel *cwRel
}

func cwHTTP(url string, limit int64) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "hostingpanel")
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 3 * time.Minute}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s answered %d", req.URL.Host, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("download is too large")
	}
	return b, nil
}

func fetchCWRel(force bool) (*cwRel, error) {
	cwCache.Lock()
	defer cwCache.Unlock()
	if !force && cwCache.rel != nil && time.Since(cwCache.at) < 10*time.Minute {
		return cwCache.rel, nil
	}
	b, err := cwHTTP(CWReleaseURL, 4<<20)
	if err != nil {
		return nil, err
	}
	var r cwRel
	if err := json.Unmarshal(b, &r); err != nil || r.Tag == "" {
		return nil, fmt.Errorf("unexpected answer from the release server")
	}
	cwCache.rel, cwCache.at = &r, time.Now()
	return &r, nil
}

func versionParts(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "-")
	if v == "" {
		return nil, false
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// newerVersion reports whether latest is a higher version than current.
func newerVersion(current, latest string) bool {
	a, ok1 := versionParts(current)
	b, ok2 := versionParts(latest)
	if !ok2 {
		return false
	}
	if !ok1 {
		return strings.TrimPrefix(current, "v") != strings.TrimPrefix(latest, "v")
	}
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return y > x
		}
	}
	return false
}

// CadWAFRelease looks up the newest published version (cached for ten minutes unless force is set).
func (m *Manager) CadWAFRelease(force bool) CWRelease {
	out := CWRelease{Current: strings.TrimSpace(m.read(cwVersion))}
	r, err := fetchCWRel(force)
	if err != nil {
		out.Error = "could not reach the release server: " + err.Error()
		return out
	}
	out.Latest = strings.TrimPrefix(r.Tag, "v")
	out.URL = r.URL
	out.Notes = r.Body
	if len(out.Notes) > 3000 {
		out.Notes = out.Notes[:3000]
	}
	out.Available = newerVersion(out.Current, out.Latest)
	return out
}

type cwBinaries struct {
	version string
	fw, ctl []byte
}

// downloadCW fetches the release archive for this CPU, verifies its SHA-256 and returns the two programs.
func downloadCW(l *tasks.Log, r *cwRel) (*cwBinaries, error) {
	ver := strings.TrimPrefix(r.Tag, "v")
	name := fmt.Sprintf("waf-firewall-%s-linux-%s.tar.gz", ver, runtime.GOARCH)
	var asset *cwAsset
	for i := range r.Assets {
		if r.Assets[i].Name == name {
			asset = &r.Assets[i]
		}
	}
	if asset == nil {
		return nil, fmt.Errorf("release %s has no build for this server (%s)", ver, runtime.GOARCH)
	}
	want := strings.ToLower(strings.TrimPrefix(asset.Digest, "sha256:"))
	if len(want) != 64 {
		want = ""
		for i := range r.Assets {
			if strings.EqualFold(r.Assets[i].Name, "SHA256SUMS.txt") {
				if sums, err := cwHTTP(r.Assets[i].URL, 1<<20); err == nil {
					for _, ln := range strings.Split(string(sums), "\n") {
						if f := strings.Fields(ln); len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
							want = strings.ToLower(f[0])
						}
					}
				}
			}
		}
	}
	if len(want) != 64 {
		return nil, fmt.Errorf("release %s has no checksum to verify the download against; refusing to install it", ver)
	}
	l.Printf("Downloading %s", name)
	data, err := cwHTTP(asset.URL, cwMaxArchive)
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		return nil, fmt.Errorf("checksum mismatch: the download is not the published release")
	}
	l.Printf("Checksum verified (%s)", want[:12])
	gz, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	b := &cwBinaries{version: ver}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		switch path.Base(h.Name) {
		case "firewalld":
			b.fw, err = io.ReadAll(io.LimitReader(tr, cwMaxBinary))
		case "firewallctl":
			b.ctl, err = io.ReadAll(io.LimitReader(tr, cwMaxBinary))
		}
		if err != nil {
			return nil, err
		}
	}
	if len(b.fw) < 1024 || len(b.ctl) < 1024 {
		return nil, fmt.Errorf("the archive does not contain firewalld and firewallctl")
	}
	return b, nil
}

func (m *Manager) putCW(b *cwBinaries) error {
	if err := m.write(cwBin, string(b.fw), 0o755); err != nil {
		return err
	}
	return m.write(cwCtl, string(b.ctl), 0o755)
}

// UpdateCadWAF installs the newest published release over the current one. If the new version does not come
// up, the previous programs are put back, so an update cannot leave the server without a working firewall.
func (m *Manager) UpdateCadWAF() (store.Task, error) {
	if !m.cwInstalled() {
		return store.Task{}, invalid("CadWAF Firewall is not installed")
	}
	return m.Tasks.Start("cadwaf.update", "Update CadWAF Firewall", func(l *tasks.Log) error {
		m.apt.Lock()
		defer m.apt.Unlock()
		l.Printf("Looking for the newest release")
		r, err := fetchCWRel(true)
		if err != nil {
			return err
		}
		cur := strings.TrimSpace(m.read(cwVersion))
		if !newerVersion(cur, strings.TrimPrefix(r.Tag, "v")) {
			l.Printf("Already on the newest version (%s)", cur)
			return nil
		}
		l.Printf("Installed %s, available %s", cur, strings.TrimPrefix(r.Tag, "v"))
		nb, err := downloadCW(l, r)
		if err != nil {
			return err
		}
		oldFW, oldCtl := []byte(m.read(cwBin)), []byte(m.read(cwCtl))
		wasActive := m.unitStates([]string{"firewalld.service"})["firewalld.service"].Active
		if err := m.putCW(nb); err != nil {
			return err
		}
		restore := func() {
			_ = m.putCW(&cwBinaries{fw: oldFW, ctl: oldCtl})
			_ = m.write(cwVersion, cur+"\n", 0o644)
			if wasActive {
				_ = m.Run.Run("systemctl", "restart", "firewalld.service")
			}
		}
		_ = m.write(cwVersion, nb.version+"\n", 0o644)
		if !wasActive {
			l.Printf("The firewall was stopped; the new version is in place. Start it when you are ready.")
			return nil
		}
		l.Printf("Restarting the firewall")
		if err := m.Run.RunW(l, "systemctl", "restart", "firewalld.service"); err != nil {
			restore()
			return fmt.Errorf("%v; the previous version was put back", err)
		}
		deadline := time.Now().Add(25 * time.Second)
		for {
			if h, err := m.cwCall("GET", "/health", nil); err == nil {
				if at, _ := h["attached"].(bool); at {
					l.Printf("Firewall %s is attached to %v", nb.version, h["iface"])
					return nil
				}
			}
			if time.Now().After(deadline) || m.Root != "" {
				break
			}
			time.Sleep(time.Second)
		}
		if m.Root != "" {
			return nil
		}
		l.Printf("The new version did not attach; restoring %s", cur)
		restore()
		return fmt.Errorf("the new version did not start; the previous version was put back")
	})
}
