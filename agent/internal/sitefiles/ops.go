package sitefiles

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
)

const (
	maxCopyBytes  = 2 << 30
	maxSearchHits = 200
)

func protected(c string) bool {
	return c == "." || c == TrashDir || strings.HasPrefix(c, TrashDir+"/")
}

// Copy duplicates a file or folder inside the jail. It refuses to overwrite and skips symlinks.
func (f *FS) Copy(from, to string) error {
	a, err1 := clean(from)
	b, err2 := clean(to)
	if err1 != nil || err2 != nil || protected(a) || protected(b) {
		return fmt.Errorf("%w: path", ErrInvalid)
	}
	if b == a || strings.HasPrefix(b, a+"/") {
		return fmt.Errorf("%w: cannot copy a folder into itself", ErrInvalid)
	}
	if _, err := f.root.Lstat(b); err == nil {
		return fmt.Errorf("%w: %s already exists", ErrInvalid, b)
	}
	var total int64
	return f.copyTree(a, b, &total)
}

func (f *FS) copyTree(a, b string, total *int64) error {
	st, err := f.root.Lstat(a)
	if err != nil {
		return mapErr(err)
	}
	switch {
	case st.Mode()&fs.ModeSymlink != 0:
		return nil
	case st.IsDir():
		if err := f.root.Mkdir(b, st.Mode().Perm()|0o700); err != nil {
			return mapErr(err)
		}
		f.chown(b)
		d, err := f.root.Open(a)
		if err != nil {
			return mapErr(err)
		}
		des, err := d.ReadDir(-1)
		d.Close()
		if err != nil {
			return mapErr(err)
		}
		for _, de := range des {
			if err := f.copyTree(path.Join(a, de.Name()), path.Join(b, de.Name()), total); err != nil {
				return err
			}
		}
		return nil
	}
	*total += st.Size()
	if *total > maxCopyBytes {
		return ErrTooLarge
	}
	src, err := f.root.Open(a)
	if err != nil {
		return mapErr(err)
	}
	defer src.Close()
	dst, err := f.root.OpenFile(b, os.O_WRONLY|os.O_CREATE|os.O_EXCL, st.Mode().Perm())
	if err != nil {
		return mapErr(err)
	}
	_, err = io.Copy(dst, src)
	dst.Close()
	f.chown(b)
	return err
}

// Archive writes a zip of paths to the file dest inside the jail.
func (f *FS) Archive(paths []string, dest string) error {
	if len(paths) == 0 {
		return fmt.Errorf("%w: nothing to archive", ErrInvalid)
	}
	for _, p := range paths {
		if c, err := clean(p); err != nil || protected(c) {
			return fmt.Errorf("%w: path", ErrInvalid)
		}
	}
	d, err := clean(dest)
	if err != nil || protected(d) || !strings.HasSuffix(strings.ToLower(d), ".zip") {
		return fmt.Errorf("%w: archive name must end with .zip", ErrInvalid)
	}
	if _, err := f.root.Lstat(d); err == nil {
		return fmt.Errorf("%w: %s already exists", ErrInvalid, d)
	}
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(f.Compress(paths, pw)) }()
	err = f.Write(d, pr, -1)
	pr.CloseWithError(err)
	return err
}

type Hit struct {
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size"`
}

// Search finds entries under dir whose name contains q (case-insensitive), skipping dependency folders.
func (f *FS) Search(dir, q string) ([]Hit, error) {
	c, err := clean(dir)
	if err != nil {
		return nil, err
	}
	q = strings.ToLower(strings.TrimSpace(q))
	if len(q) < 2 {
		return nil, fmt.Errorf("%w: type at least 2 characters", ErrInvalid)
	}
	hits := []Hit{}
	var walk func(p string, depth int)
	walk = func(p string, depth int) {
		if len(hits) >= maxSearchHits || depth > 8 {
			return
		}
		d, err := f.root.Open(p)
		if err != nil {
			return
		}
		des, _ := d.ReadDir(-1)
		d.Close()
		for _, de := range des {
			n := de.Name()
			full := path.Join(p, n)
			if full == TrashDir {
				continue
			}
			if strings.Contains(strings.ToLower(n), q) {
				var sz int64
				if info, err := de.Info(); err == nil {
					sz = info.Size()
				}
				hits = append(hits, Hit{Path: full, Dir: de.IsDir(), Size: sz})
				if len(hits) >= maxSearchHits {
					return
				}
			}
			if de.IsDir() && n != "node_modules" && n != "vendor" && n != ".git" && de.Type()&fs.ModeSymlink == 0 {
				walk(full, depth+1)
			}
		}
	}
	walk(c, 0)
	return hits, nil
}
