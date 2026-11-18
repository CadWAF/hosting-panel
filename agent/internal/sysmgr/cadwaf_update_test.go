package sysmgr

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func cwTarball(t *testing.T, ver string, fw string) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	dir := "waf-firewall-" + ver + "-linux-" + runtime.GOARCH + "/"
	for name, body := range map[string]string{"firewalld": fw, "firewallctl": strings.Repeat("c", 2048)} {
		_ = tw.WriteHeader(&tar.Header{Name: dir + name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func TestCadWAFUpdate(t *testing.T) {
	m, _ := newMgr(t)
	m.CW = NewCWFake(m)
	tk, err := m.InstallCadWAF("203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	m.Tasks.Wait()
	if got, _ := m.Tasks.Get(tk.ID); got.Status != "done" {
		t.Fatalf("install: %s", got.Log)
	}

	tar := cwTarball(t, "0.3.1", strings.Repeat("n", 4096))
	sum := sha256.Sum256(tar)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".tar.gz") {
			_, _ = w.Write(tar)
			return
		}
		name := "waf-firewall-0.3.1-linux-" + runtime.GOARCH + ".tar.gz"
		fmt.Fprintf(w, `{"tag_name":"v0.3.1","body":"notes","html_url":"https://example/r","assets":[{"name":%q,"browser_download_url":%q,"digest":%q}]}`, name, srv.URL+"/"+name, digest)
	}))
	defer srv.Close()
	old := CWReleaseURL
	CWReleaseURL = srv.URL + "/latest"
	defer func() { CWReleaseURL = old; cwCache.rel = nil }()
	cwCache.rel = nil

	rel := m.CadWAFRelease(true)
	if rel.Error != "" || !rel.Available || rel.Latest != "0.3.1" || rel.Current != "0.3.0" {
		t.Fatalf("release: %+v", rel)
	}
	tk, err = m.UpdateCadWAF()
	if err != nil {
		t.Fatal(err)
	}
	m.Tasks.Wait()
	if got, _ := m.Tasks.Get(tk.ID); got.Status != "done" {
		t.Fatalf("update: %s", got.Log)
	}
	if v := strings.TrimSpace(m.read(cwVersion)); v != "0.3.1" {
		t.Errorf("version after update: %q", v)
	}
	if len(m.read(cwBin)) != 4096 {
		t.Errorf("binary was not replaced")
	}
	if m.CadWAFRelease(true).Available {
		t.Error("still offers the installed version")
	}

	// A download that does not match the published checksum is refused and nothing changes.
	digest = "sha256:" + strings.Repeat("0", 64)
	_ = m.write(cwVersion, "0.3.0\n", 0o644)
	tk, _ = m.UpdateCadWAF()
	m.Tasks.Wait()
	if got, _ := m.Tasks.Get(tk.ID); got.Status == "done" || !strings.Contains(got.Log, "checksum") {
		t.Fatalf("bad checksum accepted: %s %s", got.Status, got.Log)
	}
	if v := strings.TrimSpace(m.read(cwVersion)); v != "0.3.0" {
		t.Errorf("version changed after a refused update: %q", v)
	}
}

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"0.3.0", "0.3.1", true}, {"0.3.0", "0.3.0", false}, {"0.3.1", "0.3.0", false}, {"0.9", "0.10", true}, {"v0.3.0", "0.3.0", false}, {"", "0.3.0", true}, {"0.3.0", "junk", false}} {
		if got := newerVersion(c.a, c.b); got != c.want {
			t.Errorf("%q -> %q: got %v", c.a, c.b, got)
		}
	}
}
