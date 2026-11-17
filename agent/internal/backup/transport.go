package backup

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Transport moves finished archive files to and from a Destination.
type Transport interface {
	Put(name, src string) error
	Get(name, dst string) error
	List() ([]string, error)
	Delete(name string) error
	Test() error
}

type localTransport struct{ dir string }

func (l localTransport) Put(name, src string) error {
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return err
	}
	if err := os.Rename(src, filepath.Join(l.dir, name)); err == nil {
		return nil
	}
	return copyFile(src, filepath.Join(l.dir, name)) // different filesystems
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func (l localTransport) Get(name, dst string) error { return copyFile(filepath.Join(l.dir, name), dst) }
func (l localTransport) Delete(name string) error   { return os.Remove(filepath.Join(l.dir, name)) }
func (l localTransport) List() ([]string, error) {
	es, err := os.ReadDir(l.dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out, err
}
func (l localTransport) Test() error {
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(l.dir, ".hp-test-*")
	if err != nil {
		return fmt.Errorf("directory is not writable: %w", err)
	}
	f.Close()
	return os.Remove(f.Name())
}

type sftpTransport struct {
	host, user, dir string
	port            int
	key             string
	hostKey         string // authorized_keys-format line, pinned
	// PinHostKey is called with the presented key when none is pinned yet (trust on first use).
	pin func(line string)
}

func (s *sftpTransport) client() (*sftp.Client, func(), error) {
	signer, err := ssh.ParsePrivateKey([]byte(s.key))
	if err != nil {
		return nil, nil, fmt.Errorf("private key: %w", err)
	}
	cb := func(_ string, _ net.Addr, k ssh.PublicKey) error {
		line := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k)))
		if s.hostKey == "" {
			if s.pin != nil {
				s.pin(line)
			}
			s.hostKey = line
			return nil
		}
		if line != s.hostKey {
			return fmt.Errorf("host key changed (possible man-in-the-middle): expected %s, got %s", ssh.FingerprintSHA256(mustParse(s.hostKey)), ssh.FingerprintSHA256(k))
		}
		return nil
	}
	conn, err := ssh.Dial("tcp", net.JoinHostPort(s.host, strconv.Itoa(s.port)), &ssh.ClientConfig{
		User: s.user, Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)}, HostKeyCallback: cb, Timeout: 20 * time.Second})
	if err != nil {
		return nil, nil, err
	}
	c, err := sftp.NewClient(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	return c, func() { c.Close(); conn.Close() }, nil
}

func mustParse(line string) ssh.PublicKey {
	k, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(line))
	return k
}

func (s *sftpTransport) Put(name, src string) error {
	c, done, err := s.client()
	if err != nil {
		return err
	}
	defer done()
	_ = c.MkdirAll(s.dir)
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := path.Join(s.dir, name+".part")
	out, err := c.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	_ = c.Remove(path.Join(s.dir, name))
	return c.Rename(tmp, path.Join(s.dir, name))
}

func (s *sftpTransport) Get(name, dst string) error {
	c, done, err := s.client()
	if err != nil {
		return err
	}
	defer done()
	in, err := c.Open(path.Join(s.dir, name))
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func (s *sftpTransport) List() ([]string, error) {
	c, done, err := s.client()
	if err != nil {
		return nil, err
	}
	defer done()
	fi, err := c.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, f := range fi {
		out = append(out, f.Name())
	}
	return out, nil
}

func (s *sftpTransport) Delete(name string) error {
	c, done, err := s.client()
	if err != nil {
		return err
	}
	defer done()
	return c.Remove(path.Join(s.dir, name))
}

func (s *sftpTransport) Test() error {
	c, done, err := s.client()
	if err != nil {
		return err
	}
	defer done()
	if err := c.MkdirAll(s.dir); err != nil {
		return err
	}
	f, err := c.Create(path.Join(s.dir, ".hp-test"))
	if err != nil {
		return fmt.Errorf("remote directory is not writable: %w", err)
	}
	f.Close()
	return c.Remove(path.Join(s.dir, ".hp-test"))
}

// archiveName builds a sortable archive file name.
func archiveName(prefix string, t time.Time) string {
	return prefix + "-" + t.UTC().Format("20060102-150405") + ".tar.gz"
}

// pruneNames returns the names to delete so that only the newest `keep` with this prefix remain.
func pruneNames(all []string, prefix string, keep int) []string {
	var mine []string
	for _, n := range all {
		if strings.HasPrefix(n, prefix+"-") && strings.HasSuffix(n, ".tar.gz") {
			mine = append(mine, n)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(mine)))
	if keep < 1 || len(mine) <= keep {
		return nil
	}
	return mine[keep:]
}

var _ = bytes.MinRead
