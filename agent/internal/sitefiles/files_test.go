package sitefiles

import (
	"archive/zip"
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newFS(t *testing.T) (*FS, string, string) {
	base := t.TempDir()
	home := filepath.Join(base, "home")
	_ = os.MkdirAll(home, 0o755)
	secret := filepath.Join(base, "secret.txt")
	_ = os.WriteFile(secret, []byte("top secret"), 0o644)
	f, err := Open(home, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f, home, secret
}

func TestCannotEscapeViaDotDotOrSymlink(t *testing.T) {
	f, home, secret := newFS(t)
	if _, err := f.ReadText("../secret.txt"); err != nil && errors.Is(err, nil) {
		t.Fatal()
	}
	// ".." is clamped to the root, so this resolves to a missing file INSIDE the jail.
	if _, err := f.ReadText("../../secret.txt"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dotdot must stay inside the jail, got %v", err)
	}
	if err := os.Symlink(secret, filepath.Join(home, "link")); err != nil {
		t.Fatal(err)
	}
	if b, err := f.ReadText("link"); err == nil {
		t.Fatalf("symlink escape leaked %q", b)
	}
	if err := f.Write("link", strings.NewReader("pwned"), 100); err == nil {
		got, _ := os.ReadFile(secret)
		if string(got) != "top secret" {
			t.Fatal("write through symlink modified a file outside the jail")
		}
	}
}

func TestWriteListRenameTrash(t *testing.T) {
	f, home, _ := newFS(t)
	if err := f.Mkdir("a"); err != nil {
		t.Fatal(err)
	}
	if err := f.Write("a/x.php", strings.NewReader("<?php echo 1;"), 1000); err != nil {
		t.Fatal(err)
	}
	if err := f.Write("a/x.php", strings.NewReader("<?php echo 2;"), 1000); err != nil {
		t.Fatal(err)
	}
	b, _ := f.ReadText("a/x.php")
	if b != "<?php echo 2;" {
		t.Fatal(b)
	}
	es, _ := f.List("a")
	if len(es) != 1 || es[0].Name != "x.php" || es[0].Mode != "0644" {
		t.Fatalf("%+v", es)
	}
	if err := f.Rename("a/x.php", "a/y.php"); err != nil {
		t.Fatal(err)
	}
	if err := f.Rename("a/y.php", "a"); err == nil {
		t.Fatal("rename must not overwrite")
	}
	if err := f.Trash("a/y.php"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "a", "y.php")); !os.IsNotExist(err) {
		t.Fatal("file should have moved")
	}
	root, _ := f.List("")
	for _, e := range root {
		if e.Name == TrashDir {
			t.Fatal("trash must be hidden from the listing")
		}
	}
	if err := f.Write("big", strings.NewReader(strings.Repeat("x", 50)), 10); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("limit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "big.hp-upload")); !os.IsNotExist(err) {
		t.Fatal("temp file must be cleaned up")
	}
}

func TestExtractRejectsZipSlipAndCompressRoundTrip(t *testing.T) {
	f, home, _ := newFS(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../../evil.txt")
	_, _ = w.Write([]byte("evil"))
	w2, _ := zw.Create("ok/fine.txt")
	_, _ = w2.Write([]byte("fine"))
	zw.Close()
	_ = os.WriteFile(filepath.Join(home, "p.zip"), buf.Bytes(), 0o644)
	_ = f.Extract("p.zip", "out", 1<<20)
	if _, err := os.Stat(filepath.Join(filepath.Dir(home), "evil.txt")); !os.IsNotExist(err) {
		t.Fatal("zip slip escaped the jail")
	}
	if b, _ := os.ReadFile(filepath.Join(home, "out", "ok", "fine.txt")); string(b) != "fine" {
		t.Fatal("legit entry should be extracted")
	}
	var out bytes.Buffer
	if err := f.Compress([]string{"out"}, &out); err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil || len(r.File) == 0 {
		t.Fatalf("bad zip: %v", err)
	}
}

func TestBinaryAndTooLargeNotEditable(t *testing.T) {
	f, home, _ := newFS(t)
	_ = os.WriteFile(filepath.Join(home, "b.bin"), []byte{0xff, 0xfe, 0, 1}, 0o644)
	if _, err := f.ReadText("b.bin"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("binary: %v", err)
	}
	_ = os.WriteFile(filepath.Join(home, "big.txt"), bytes.Repeat([]byte("a"), MaxEditSize+1), 0o644)
	if _, err := f.ReadText("big.txt"); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("large: %v", err)
	}
}

func TestCopySearchArchive(t *testing.T) {
	dir := t.TempDir()
	f, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := f.Mkdir("a"); err != nil {
		t.Fatal("mkdir", err)
	}
	if err := f.Write("a/one.txt", strings.NewReader("1"), 100); err != nil {
		t.Fatal("write", err)
	}
	if err := f.Copy("a", "b"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b/one.txt")); err != nil {
		t.Fatal("copy missing")
	}
	if err := f.Copy("a", "b"); !errors.Is(err, ErrInvalid) {
		t.Errorf("overwrite allowed: %v", err)
	}
	if err := f.Copy("a", "a/inner"); !errors.Is(err, ErrInvalid) {
		t.Errorf("copy into itself allowed: %v", err)
	}
	h, _ := f.Search("", "ONE")
	if len(h) != 2 {
		t.Errorf("hits %+v", h)
	}
	if err := f.Archive([]string{"a", "b"}, "out.zip"); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Join(dir, "out.zip")); err != nil || st.Size() == 0 {
		t.Fatal("zip not written")
	}
	if err := f.Archive([]string{"a"}, "../out.zip"); err == nil {
		t.Error("archive escape allowed")
	}
}
