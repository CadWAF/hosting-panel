package dbmanager

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/system"
	"hostingpanel/agent/internal/tasks"
)

func setup(t *testing.T, installed bool) (*Manager, *system.DryRunner) {
	st, err := store.Open(filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := &system.DryRunner{}
	m, err := New(r, tasks.New(st), st)
	if err != nil {
		t.Fatal(err)
	}
	m.Installed = func(string) bool { return installed }
	return m, r
}

func TestCreateSendsSQLOnStdinAndReturnsPasswordOnce(t *testing.T) {
	m, r := setup(t, true)
	d, pass, err := m.Create("mariadb", "shop", "shop.example.com")
	if err != nil || len(pass) != 32 {
		t.Fatalf("%v %q", err, pass)
	}
	if len(r.Stdin) != 1 || !strings.Contains(r.Stdin[0], "CREATE USER 'shop'@'localhost'") {
		t.Fatalf("sql: %v", r.Stdin)
	}
	for _, c := range r.Calls {
		if strings.Contains(c, pass) {
			t.Fatal("password must never appear in argv")
		}
	}
	l, _ := m.List()
	if len(l) != 1 || l[0].Site != "shop.example.com" || d.ID == 0 {
		t.Fatalf("%+v", l)
	}
	if _, _, err := m.Create("mariadb", "shop", ""); !errors.Is(err, ErrExists) {
		t.Fatalf("want ErrExists, got %v", err)
	}
}

func TestCreateRejectsInjectionAndMissingServer(t *testing.T) {
	m, _ := setup(t, true)
	for _, bad := range []string{"", "Shop", "a;drop", "1abc", "x`y", strings.Repeat("a", 33)} {
		if _, _, err := m.Create("mariadb", bad, ""); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q accepted: %v", bad, err)
		}
	}
	m2, _ := setup(t, false)
	if _, _, err := m2.Create("mariadb", "ok", ""); !errors.Is(err, ErrNoServer) {
		t.Errorf("want ErrNoServer, got %v", err)
	}
}

func TestPostgresUsesPsqlAndDeleteDrops(t *testing.T) {
	m, r := setup(t, true)
	d, _, err := m.Create("postgresql", "pg_app", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Calls[0], "runuser -u postgres -- psql") || !strings.Contains(r.Stdin[0], "CREATE ROLE pg_app") {
		t.Fatalf("%v %v", r.Calls, r.Stdin)
	}
	if err := m.Delete(d.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Stdin[1], "DROP DATABASE IF EXISTS pg_app") {
		t.Fatal(r.Stdin[1])
	}
	if l, _ := m.List(); len(l) != 0 {
		t.Fatal("should be gone")
	}
}

func TestInstallEngineRules(t *testing.T) {
	m, _ := setup(t, false)
	if _, err := m.InstallEngine("mariadb", "9.9"); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad version: %v", err)
	}
	if _, err := m.InstallEngine("oracle", "1"); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad engine: %v", err)
	}
	if _, err := m.InstallEngine("mariadb", "11.4"); err != nil {
		t.Fatal(err)
	}
	m.tasks.Wait()
	m.Installed = func(string) bool { return true }
	if _, err := m.InstallEngine("mariadb", "11.4"); !errors.Is(err, ErrExists) {
		t.Errorf("second install must be refused: %v", err)
	}
}
