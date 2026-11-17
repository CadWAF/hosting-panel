package backup

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/system"
	"hostingpanel/agent/internal/tasks"
)

func TestDue(t *testing.T) {
	at := func(s string) time.Time { x, _ := time.Parse("2006-01-02 15:04", s); return x }
	cases := []struct {
		expr, when string
		want       bool
	}{
		{"0 3 * * *", "2026-11-17 03:00", true},
		{"0 3 * * *", "2026-11-17 03:01", false},
		{"*/15 * * * *", "2026-11-17 10:45", true},
		{"*/15 * * * *", "2026-11-17 10:46", false},
		{"0 3 * * 0", "2026-11-22 03:00", true}, // Sunday
		{"0 3 * * 0", "2026-11-17 03:00", false},
		{"30 1-5 1,15 * *", "2026-11-15 02:30", true},
		{"30 1-5 1,15 * *", "2026-11-16 02:30", false},
	}
	for _, c := range cases {
		got, err := Due(c.expr, at(c.when))
		if err != nil || got != c.want {
			t.Errorf("%q at %s: got %v err %v, want %v", c.expr, c.when, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "* * * *", "61 * * * *", "a * * * *", "*/0 * * * *"} {
		if _, err := Due(bad, time.Now()); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	all := []string{"a-j1-20260101-000000.tar.gz", "a-j1-20260103-000000.tar.gz", "a-j1-20260102-000000.tar.gz", "other-j2-20250101-000000.tar.gz", "notes.txt"}
	got := pruneNames(all, "a-j1", 2)
	if len(got) != 1 || got[0] != "a-j1-20260101-000000.tar.gz" {
		t.Fatalf("%v", got)
	}
	if pruneNames(all, "a-j1", 5) != nil {
		t.Fatal("nothing to prune")
	}
}

func newMgr(t *testing.T, run system.Runner) (*Manager, string) {
	base := t.TempDir()
	st, err := store.Open(filepath.Join(base, "b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	m, err := New(st, run, tasks.New(st), filepath.Join(base, "state"))
	if err != nil {
		t.Fatal(err)
	}
	m.SitesBase = filepath.Join(base, "sites")
	tick := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m.now = func() time.Time { tick = tick.Add(time.Hour); return tick }
	return m, base
}

func TestLocalBackupRestoreRoundTripWithRealTar(t *testing.T) {
	m, base := newMgr(t, system.ExecRunner{})
	home := filepath.Join(base, "sites", "s_x")
	_ = os.MkdirAll(filepath.Join(home, "public_html"), 0o755)
	idx := filepath.Join(home, "public_html", "index.php")
	_ = os.WriteFile(idx, []byte("version 1"), 0o644)

	dest, err := m.AddDestination(DestinationInput{Name: "disk", Type: "local", Config: map[string]string{"path": filepath.Join(base, "backups")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.TestDestination(dest.ID); err != nil {
		t.Fatal(err)
	}
	j, err := m.AddJob(Job{Name: "x daily", Site: "x.example.com", HomePath: home, DestinationID: dest.ID, Keep: 2})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := m.RunJob(j.ID); err != nil {
			t.Fatal(err)
		}
		m.tasks.Wait()
	}
	runs, _ := m.Runs(j.ID)
	ok, pruned := 0, 0
	for _, r := range runs {
		switch r.Status {
		case "ok":
			ok++
		case "pruned":
			pruned++
		}
	}
	if ok != 2 || pruned != 1 {
		t.Fatalf("keep=2 of 3 runs: ok=%d pruned=%d %+v", ok, pruned, runs)
	}
	files, _ := os.ReadDir(filepath.Join(base, "backups"))
	if len(files) != 2 {
		t.Fatalf("archives on disk: %d", len(files))
	}

	_ = os.WriteFile(idx, []byte("broken"), 0o644)
	var latest Run
	for _, r := range runs {
		if r.Status == "ok" {
			latest = r
			break
		}
	}
	if _, err := m.Restore(latest.ID, RestoreInput{Mode: "over", Files: true}); err != nil {
		t.Fatal(err)
	}
	m.tasks.Wait()
	if b, _ := os.ReadFile(idx); string(b) != "version 1" {
		t.Fatalf("restore over did not bring the file back: %q", b)
	}

	_ = os.WriteFile(idx, []byte("live edits"), 0o644)
	if _, err := m.Restore(latest.ID, RestoreInput{Mode: "beside", Files: true}); err != nil {
		t.Fatal(err)
	}
	m.tasks.Wait()
	if b, _ := os.ReadFile(idx); string(b) != "live edits" {
		t.Fatal("restore beside must not touch live files")
	}
	matches, _ := filepath.Glob(filepath.Join(home, "restore-*", "public_html", "index.php"))
	if len(matches) != 1 {
		t.Fatalf("restored copy missing: %v", matches)
	}
	if _, err := m.Restore(latest.ID, RestoreInput{Mode: "beside", Databases: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("db beside must be refused: %v", err)
	}
}

func TestValidation(t *testing.T) {
	m, base := newMgr(t, &system.DryRunner{})
	d, _ := m.AddDestination(DestinationInput{Name: "d", Type: "local", Config: map[string]string{"path": filepath.Join(base, "b")}})
	for name, j := range map[string]Job{
		"home outside sites": {Name: "a", Site: "x", HomePath: "/etc", DestinationID: d.ID, Keep: 1},
		"traversal":          {Name: "a", Site: "x", HomePath: m.SitesBase + "/../etc", DestinationID: d.ID, Keep: 1},
		"nothing selected":   {Name: "a", Site: "x", DestinationID: d.ID, Keep: 1},
		"bad db name":        {Name: "a", Site: "x", Databases: []DBRef{{"mariadb", "x;drop"}}, DestinationID: d.ID, Keep: 1},
		"bad schedule":       {Name: "a", Site: "x", Databases: []DBRef{{"mariadb", "ok"}}, DestinationID: d.ID, Keep: 1, Schedule: "nope"},
		"keep zero":          {Name: "a", Site: "x", Databases: []DBRef{{"mariadb", "ok"}}, DestinationID: d.ID, Keep: 0},
		"missing dest":       {Name: "a", Site: "x", Databases: []DBRef{{"mariadb", "ok"}}, DestinationID: 99, Keep: 1},
	} {
		if _, err := m.AddJob(j); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	for name, in := range map[string]DestinationInput{
		"relative local": {Name: "x", Type: "local", Config: map[string]string{"path": "rel"}},
		"root local":     {Name: "x", Type: "local", Config: map[string]string{"path": "/"}},
		"sftp no key":    {Name: "x", Type: "sftp", Config: map[string]string{"host": "h", "user": "u", "path": "/p"}},
		"borg short pw":  {Name: "x", Type: "borg", Config: map[string]string{"repo": "/r"}, Passphrase: "short"},
		"borg bad repo":  {Name: "x", Type: "borg", Config: map[string]string{"repo": "/r; rm -rf /"}, Passphrase: "longenough"},
		"remote no key":  {Name: "x", Type: "borg", Config: map[string]string{"repo": "ssh://u@h/r"}, Passphrase: "longenough"},
	} {
		if _, err := m.AddDestination(in); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	if err := m.DeleteDestination(d.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDatabaseDumpPassesNamesAsArgsNotShellText(t *testing.T) {
	r := &system.DryRunner{}
	m, base := newMgr(t, r)
	d, _ := m.AddDestination(DestinationInput{Name: "d", Type: "local", Config: map[string]string{"path": filepath.Join(base, "b")}})
	j, err := m.AddJob(Job{Name: "db", Site: "x.com", Databases: []DBRef{{"mariadb", "shop"}, {"postgresql", "pg"}}, DestinationID: d.ID, Keep: 3})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = m.RunJob(j.ID)
	m.tasks.Wait()
	joined := strings.Join(r.Calls, "\n")
	for _, want := range []string{"mariadb-dump --single-transaction", "pg_dump"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "shop |") {
		t.Error("db name must be a positional argument, not interpolated")
	}
}

func TestSchedulerRunsDueJobOncePerMinute(t *testing.T) {
	r := &system.DryRunner{}
	m, base := newMgr(t, r)
	d, _ := m.AddDestination(DestinationInput{Name: "d", Type: "local", Config: map[string]string{"path": filepath.Join(base, "b")}})
	_, _ = m.AddJob(Job{Name: "n", Site: "x.com", Databases: []DBRef{{"mariadb", "a"}}, DestinationID: d.ID, Keep: 1, Schedule: "0 3 * * *"})
	at := time.Date(2026, 5, 5, 3, 0, 0, 0, time.UTC)
	m.Tick(at)
	m.tasks.Wait()
	m.Tick(at)
	m.tasks.Wait()
	m.Tick(at.Add(time.Minute))
	m.tasks.Wait()
	runs, _ := m.Runs(0)
	if len(runs) != 1 {
		t.Fatalf("want exactly 1 run, got %d", len(runs))
	}
}

// ---- SFTP against an in-process server ----

func startSFTPServer(t *testing.T, root string) (port int, clientKeyPEM, hostLine string) {
	hostPub, hostPriv, _ := ed25519.GenerateKey(rand.Reader)
	hs, _ := ssh.NewSignerFromKey(hostPriv)
	hp, _ := ssh.NewPublicKey(hostPub)
	hostLine = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(hp)))

	cPub, cPriv, _ := ed25519.GenerateKey(rand.Reader)
	cs, _ := ssh.NewPublicKey(cPub)
	blk, _ := ssh.MarshalPrivateKey(&cPriv, "")
	clientKeyPEM = string(pem.EncodeToMemory(blk))

	cfg := &ssh.ServerConfig{PublicKeyCallback: func(_ ssh.ConnMetadata, k ssh.PublicKey) (*ssh.Permissions, error) {
		if string(k.Marshal()) == string(cs.Marshal()) {
			return nil, nil
		}
		return nil, errors.New("unknown key")
	}}
	cfg.AddHostKey(hs)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				_, chans, reqs, err := ssh.NewServerConn(c, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				for nc := range chans {
					if nc.ChannelType() != "session" {
						_ = nc.Reject(ssh.UnknownChannelType, "")
						continue
					}
					ch, creqs, _ := nc.Accept()
					go func() {
						for r := range creqs {
							ok := r.Type == "subsystem"
							_ = r.Reply(ok, nil)
							if ok {
								srv, _ := sftp.NewServer(ch, sftp.WithServerWorkingDirectory(root))
								_ = srv.Serve()
								ch.Close()
							}
						}
					}()
				}
			}()
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, clientKeyPEM, hostLine
}

func TestSFTPBackupRestoreWithHostKeyPinning(t *testing.T) {
	m, base := newMgr(t, system.ExecRunner{})
	remote := filepath.Join(base, "remote")
	_ = os.MkdirAll(remote, 0o755)
	port, key, hostLine := startSFTPServer(t, remote)

	home := filepath.Join(base, "sites", "s_y")
	_ = os.MkdirAll(home, 0o755)
	_ = os.WriteFile(filepath.Join(home, "a.txt"), []byte("hello sftp"), 0o644)

	d, err := m.AddDestination(DestinationInput{Name: "remote", Type: "sftp", Key: key,
		Config: map[string]string{"host": "127.0.0.1", "port": strconv.Itoa(port), "user": "backup", "path": remote + "/dumps"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.TestDestination(d.ID); err != nil {
		t.Fatalf("test: %v", err)
	}
	got, _ := m.Destinations()
	if got[0].Config["host_key"] != hostLine {
		t.Fatalf("host key must be pinned on first use, got %q", got[0].Config["host_key"])
	}
	if got[0].HasKey != true {
		t.Fatal("has_secret flag")
	}
	b, _ := json_marshal(got[0])
	if strings.Contains(b, "OPENSSH PRIVATE KEY") {
		t.Fatal("private key leaked through the API model")
	}

	j, _ := m.AddJob(Job{Name: "y", Site: "y.com", HomePath: home, DestinationID: d.ID, Keep: 5})
	if _, err := m.RunJob(j.ID); err != nil {
		t.Fatal(err)
	}
	m.tasks.Wait()
	runs, _ := m.Runs(j.ID)
	if len(runs) != 1 || runs[0].Status != "ok" {
		t.Fatalf("%+v", runs)
	}
	entries, _ := os.ReadDir(filepath.Join(remote, "dumps"))
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), ".tar.gz") {
		t.Fatalf("remote files: %v", entries)
	}
	_ = os.WriteFile(filepath.Join(home, "a.txt"), []byte("lost"), 0o644)
	if _, err := m.Restore(runs[0].ID, RestoreInput{Mode: "over", Files: true}); err != nil {
		t.Fatal(err)
	}
	m.tasks.Wait()
	if b, _ := os.ReadFile(filepath.Join(home, "a.txt")); string(b) != "hello sftp" {
		t.Fatalf("restore from sftp: %q", b)
	}

	// A different host key after pinning must be refused.
	m.pinHostKey(d.ID, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl")
	if err := m.TestDestination(d.ID); err == nil || !strings.Contains(err.Error(), "host key changed") {
		t.Fatalf("changed host key must be rejected, got %v", err)
	}
}

func json_marshal(v any) (string, error) {
	var sb strings.Builder
	err := jsonEncode(&sb, v)
	return sb.String(), err
}

func jsonEncode(w io.Writer, v any) error { return jsonNewEncoder(w).Encode(v) }

var jsonNewEncoder = func(w io.Writer) interface{ Encode(any) error } { return newEnc(w) }
