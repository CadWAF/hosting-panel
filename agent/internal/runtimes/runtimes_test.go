package runtimes

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/system"
	"hostingpanel/agent/internal/tasks"
)

func setup(t *testing.T) (*Manager, *system.DryRunner, *store.Store, string) {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := &system.DryRunner{}
	return New(r, tasks.New(st), st, filepath.Join(dir, "php")), r, st, filepath.Join(dir, "php")
}

func TestInstallRunsAptAndTaskCompletes(t *testing.T) {
	m, r, st, _ := setup(t)
	task, err := m.Install("8.2")
	if err != nil {
		t.Fatal(err)
	}
	m.tasks.Wait()
	got, _ := st.GetTask(task.ID)
	if got.Status != "done" || !strings.Contains(got.Log, "Installing PHP 8.2") {
		t.Fatalf("task: %+v", got)
	}
	if !strings.Contains(strings.Join(r.Calls, "\n"), "php8.2-fpm") {
		t.Errorf("calls: %v", r.Calls)
	}
	if _, err := m.Install("9.9"); !errors.Is(err, ErrInvalid) {
		t.Errorf("want invalid, got %v", err)
	}
}

func TestRemoveBlockedWhileSitesUseIt(t *testing.T) {
	m, _, st, _ := setup(t)
	_ = st.Insert(store.Site{Domain: "a.com", User: "s_a", PHPVersion: "8.1", Status: "enabled"})
	if _, err := m.Remove("8.1"); !errors.Is(err, ErrInUse) {
		t.Fatalf("want ErrInUse, got %v", err)
	}
	if _, err := m.Remove("8.2"); err != nil {
		t.Fatalf("unused runtime should be removable: %v", err)
	}
	m.tasks.Wait()
}

func TestExtensionsAreAllowlisted(t *testing.T) {
	m, _, _, php := setup(t)
	if _, err := m.InstallExtension("8.3", "redis"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("not installed runtime must be rejected: %v", err)
	}
	_ = os.MkdirAll(filepath.Join(php, "8.3", "fpm"), 0o755)
	if _, err := m.InstallExtension("8.3", "redis; rm -rf /"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("injection must be rejected: %v", err)
	}
	if _, err := m.InstallExtension("8.3", "redis"); err != nil {
		t.Fatal(err)
	}
	m.tasks.Wait()
}
