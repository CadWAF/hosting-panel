// Package sitefiles is the File Manager backend. Every operation is confined to one Site's
// home directory using os.Root, which refuses ".." and symlinks that point outside it.
package sitefiles

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalid  = errors.New("invalid input")
	ErrNotFound = errors.New("not found")
	ErrTooLarge = errors.New("file is too large")
)

const (
	MaxEditSize = 2 << 20
	TrashDir    = ".trash"
)

type Entry struct {
	Name    string    `json:"name"`
	Dir     bool      `json:"dir"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"mod_time"`
	Symlink bool      `json:"symlink"`
}

// FS is a jailed view of one directory. Chown is called for every file we create so it is owned by the Site User.
type FS struct {
	root  *os.Root
	chown func(rel string)
}

func Open(home string, chown func(rel string)) (*FS, error) {
	r, err := os.OpenRoot(home)
	if err != nil {
		return nil, err
	}
	if chown == nil {
		chown = func(string) {}
	}
	return &FS{root: r, chown: chown}, nil
}

func (f *FS) Close() error { return f.root.Close() }

// clean turns a user path into a root-relative one; "" and "/" mean the home itself.
func clean(p string) (string, error) {
	if strings.ContainsRune(p, 0) {
		return "", ErrInvalid
	}
	c := path.Clean("/" + p)
	c = strings.TrimPrefix(c, "/")
	if c == "" {
		c = "."
	}
	return c, nil
}

func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return ErrNotFound
	case strings.Contains(err.Error(), "path escapes"):
		return fmt.Errorf("%w: path leaves the site directory", ErrInvalid)
	}
	return err
}

func (f *FS) List(p string) ([]Entry, error) {
	c, err := clean(p)
	if err != nil {
		return nil, err
	}
	d, err := f.root.Open(c)
	if err != nil {
		return nil, mapErr(err)
	}
	defer d.Close()
	des, err := d.ReadDir(-1)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		if c == "." && de.Name() == TrashDir {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		out = append(out, Entry{Name: de.Name(), Dir: de.IsDir(), Size: info.Size(), Mode: fmt.Sprintf("%04o", info.Mode().Perm()),
			ModTime: info.ModTime().UTC(), Symlink: info.Mode()&fs.ModeSymlink != 0})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Dir != out[j].Dir {
			return out[i].Dir
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// ReadText returns a file for editing; binary or oversized files are refused.
func (f *FS) ReadText(p string) (string, error) {
	c, err := clean(p)
	if err != nil {
		return "", err
	}
	fh, err := f.root.Open(c)
	if err != nil {
		return "", mapErr(err)
	}
	defer fh.Close()
	st, err := fh.Stat()
	if err != nil {
		return "", err
	}
	if st.IsDir() {
		return "", fmt.Errorf("%w: is a directory", ErrInvalid)
	}
	if st.Size() > MaxEditSize {
		return "", ErrTooLarge
	}
	b, err := io.ReadAll(fh)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) || strings.ContainsRune(string(b), 0) {
		return "", fmt.Errorf("%w: not a text file", ErrInvalid)
	}
	return string(b), nil
}

// Open returns a reader for download.
func (f *FS) OpenRead(p string) (*os.File, os.FileInfo, error) {
	c, err := clean(p)
	if err != nil {
		return nil, nil, err
	}
	fh, err := f.root.Open(c)
	if err != nil {
		return nil, nil, mapErr(err)
	}
	st, err := fh.Stat()
	if err != nil || st.IsDir() {
		fh.Close()
		return nil, nil, fmt.Errorf("%w: not a file", ErrInvalid)
	}
	return fh, st, nil
}

// Write creates or replaces a file from r (limit < 0 means unlimited).
func (f *FS) Write(p string, r io.Reader, limit int64) error {
	c, err := clean(p)
	if err != nil || c == "." {
		return fmt.Errorf("%w: path", ErrInvalid)
	}
	mode := fs.FileMode(0o644)
	if st, err := f.root.Lstat(c); err == nil {
		mode = st.Mode().Perm()
	}
	tmp := c + ".hp-upload"
	fh, err := f.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return mapErr(err)
	}
	var src io.Reader = r
	if limit >= 0 {
		src = io.LimitReader(r, limit+1)
	}
	n, err := io.Copy(fh, src)
	fh.Close()
	if err == nil && limit >= 0 && n > limit {
		err = ErrTooLarge
	}
	if err != nil {
		_ = f.root.Remove(tmp)
		return err
	}
	if err := f.root.Rename(tmp, c); err != nil {
		_ = f.root.Remove(tmp)
		return mapErr(err)
	}
	f.chown(c)
	return nil
}

func (f *FS) Mkdir(p string) error {
	c, err := clean(p)
	if err != nil || c == "." {
		return fmt.Errorf("%w: path", ErrInvalid)
	}
	if err := f.root.Mkdir(c, 0o755); err != nil {
		return mapErr(err)
	}
	f.chown(c)
	return nil
}

// Rename moves or renames inside the jail. It refuses to overwrite.
func (f *FS) Rename(from, to string) error {
	a, err1 := clean(from)
	b, err2 := clean(to)
	if err1 != nil || err2 != nil || a == "." || b == "." || strings.HasPrefix(a, TrashDir) || strings.HasPrefix(b, TrashDir) {
		return fmt.Errorf("%w: path", ErrInvalid)
	}
	if _, err := f.root.Lstat(b); err == nil {
		return fmt.Errorf("%w: %s already exists", ErrInvalid, b)
	}
	return mapErr(f.root.Rename(a, b))
}

// Trash moves an entry into the Site's .trash instead of erasing it.
func (f *FS) Trash(p string) error {
	c, err := clean(p)
	if err != nil || c == "." || c == TrashDir || strings.HasPrefix(c, TrashDir+"/") {
		return fmt.Errorf("%w: path", ErrInvalid)
	}
	if err := f.root.Mkdir(TrashDir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return mapErr(err)
	}
	f.chown(TrashDir)
	dst := fmt.Sprintf("%s/%d-%s", TrashDir, time.Now().UnixNano(), path.Base(c))
	return mapErr(f.root.Rename(c, dst))
}

func (f *FS) Chmod(p string, mode uint32) error {
	c, err := clean(p)
	if err != nil || mode > 0o777 {
		return fmt.Errorf("%w: mode", ErrInvalid)
	}
	return mapErr(f.root.Chmod(c, fs.FileMode(mode)))
}

// Extract unpacks a zip next to itself (into dest). Entries that would leave dest are rejected by os.Root.
func (f *FS) Extract(zipPath, dest string, maxBytes int64) error {
	zc, err := clean(zipPath)
	if err != nil {
		return err
	}
	dc, err := clean(dest)
	if err != nil {
		return err
	}
	fh, err := f.root.Open(zc)
	if err != nil {
		return mapErr(err)
	}
	defer fh.Close()
	st, _ := fh.Stat()
	zr, err := zip.NewReader(fh, st.Size())
	if err != nil {
		return fmt.Errorf("%w: not a zip file", ErrInvalid)
	}
	var total int64
	for _, e := range zr.File {
		name, err := clean(path.Join(dc, e.Name))
		if err != nil {
			return err
		}
		if e.FileInfo().IsDir() {
			if err := f.mkdirAll(name); err != nil {
				return err
			}
			continue
		}
		if e.Mode()&fs.ModeSymlink != 0 {
			continue // never recreate symlinks from archives
		}
		total += int64(e.UncompressedSize64)
		if total > maxBytes {
			return ErrTooLarge
		}
		if err := f.mkdirAll(path.Dir(name)); err != nil {
			return err
		}
		rc, err := e.Open()
		if err != nil {
			return err
		}
		err = f.Write(name, rc, maxBytes)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func (f *FS) mkdirAll(p string) error {
	if p == "." || p == "" {
		return nil
	}
	parts := strings.Split(p, "/")
	cur := ""
	for _, s := range parts {
		cur = path.Join(cur, s)
		if err := f.root.Mkdir(cur, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return mapErr(err)
		}
		f.chown(cur)
	}
	return nil
}

// Compress writes a zip containing the given paths (files or directories) to out.
func (f *FS) Compress(paths []string, out io.Writer) error {
	zw := zip.NewWriter(out)
	for _, p := range paths {
		c, err := clean(p)
		if err != nil {
			return err
		}
		if err := f.addToZip(zw, c); err != nil {
			return err
		}
	}
	return zw.Close()
}

func (f *FS) addToZip(zw *zip.Writer, c string) error {
	st, err := f.root.Lstat(c)
	if err != nil {
		return mapErr(err)
	}
	if st.Mode()&fs.ModeSymlink != 0 {
		return nil
	}
	if st.IsDir() {
		des, err := func() ([]fs.DirEntry, error) {
			d, err := f.root.Open(c)
			if err != nil {
				return nil, err
			}
			defer d.Close()
			return d.ReadDir(-1)
		}()
		if err != nil {
			return mapErr(err)
		}
		for _, de := range des {
			if err := f.addToZip(zw, path.Join(c, de.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	w, err := zw.Create(c)
	if err != nil {
		return err
	}
	fh, err := f.root.Open(c)
	if err != nil {
		return mapErr(err)
	}
	defer fh.Close()
	_, err = io.Copy(w, fh)
	return err
}
