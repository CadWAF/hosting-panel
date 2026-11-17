package sites

import (
	"bufio"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHGroup is the Unix group of all Site Users; sshd restricts it to key-only login.
const SSHGroup = "hostingpanel-sites"

type SSHKey struct {
	ID          int64     `json:"id"`
	Domain      string    `json:"domain"`
	Label       string    `json:"label"`
	Fingerprint string    `json:"fingerprint"`
	PublicKey   string    `json:"public_key"`
	AddedAt     time.Time `json:"added_at"`
}

func (s *Service) initAccess() error {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS ssh_keys(id INTEGER PRIMARY KEY AUTOINCREMENT, domain TEXT NOT NULL,
			label TEXT NOT NULL, fingerprint TEXT NOT NULL, public_key TEXT NOT NULL, added_at TIMESTAMP NOT NULL,
			UNIQUE(domain, fingerprint))`,
		`CREATE TABLE IF NOT EXISTS cron_jobs(id INTEGER PRIMARY KEY AUTOINCREMENT, domain TEXT NOT NULL,
			schedule TEXT NOT NULL, command TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1)`,
	} {
		if _, err := s.st.DB().Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// SSHConfigSnippet forces key-only login for every Site User.
const SSHConfigSnippet = `# managed by hostingpanel core-agent - do not edit
Match Group ` + SSHGroup + `
    PasswordAuthentication no
    KbdInteractiveAuthentication no
    AuthenticationMethods publickey
    PermitRootLogin no
    X11Forwarding no
`

// EnsureSSHConfig installs the sshd drop-in and the group. Safe to call repeatedly.
func (s *Service) EnsureSSHConfig() error {
	_ = s.run.Run("groupadd", "--force", SSHGroup)
	path := filepath.Join(s.p.Root, "/etc/ssh/sshd_config.d/50-hostingpanel.conf")
	if b, _ := os.ReadFile(path); string(b) == SSHConfigSnippet {
		return nil
	}
	if err := write(path, SSHConfigSnippet); err != nil {
		return err
	}
	if err := s.run.Run("sshd", "-t"); err != nil {
		_ = os.Remove(path)
		return err
	}
	return s.run.Run("systemctl", "reload", "ssh")
}

var labelRe = regexp.MustCompile(`^[\p{L}\p{N} ._@-]{1,60}$`)

// parseKey accepts exactly one plain public key line (no options such as command="...").
func parseKey(line string) (ssh.PublicKey, string, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.ContainsAny(line, "\r\n") {
		return nil, "", fmt.Errorf("%w: paste exactly one public key", ErrInvalid)
	}
	pub, comment, opts, _, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil || len(opts) > 0 {
		return nil, "", fmt.Errorf("%w: not a valid public key (options are not allowed)", ErrInvalid)
	}
	switch pub.Type() {
	case ssh.KeyAlgoED25519, ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521, ssh.KeyAlgoSKED25519, ssh.KeyAlgoSKECDSA256:
	case ssh.KeyAlgoRSA:
		if cp, ok := pub.(ssh.CryptoPublicKey); ok {
			if rk, ok := cp.CryptoPublicKey().(interface{ Size() int }); ok && rk.Size()*8 < 2048 {
				return nil, "", fmt.Errorf("%w: RSA keys must be at least 2048 bits", ErrInvalid)
			}
		}
	default:
		return nil, "", fmt.Errorf("%w: key type %s is not allowed", ErrInvalid, pub.Type())
	}
	return pub, comment, nil
}

func (s *Service) ListKeys(domain string) ([]SSHKey, error) {
	if _, err := s.st.Get(domain); err != nil {
		return nil, err
	}
	rows, err := s.st.DB().Query(`SELECT id,domain,label,fingerprint,public_key,added_at FROM ssh_keys WHERE domain=? ORDER BY id`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SSHKey{}
	for rows.Next() {
		var k SSHKey
		if err := rows.Scan(&k.ID, &k.Domain, &k.Label, &k.Fingerprint, &k.PublicKey, &k.AddedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// AddKey stores an existing public key for the Site User.
func (s *Service) AddKey(domain, label, public string) (SSHKey, error) {
	x, err := s.st.Get(domain)
	if err != nil {
		return SSHKey{}, err
	}
	pub, comment, err := parseKey(public)
	if err != nil {
		return SSHKey{}, err
	}
	if label = strings.TrimSpace(label); label == "" {
		label = comment
	}
	if label == "" {
		label = "key"
	}
	if !labelRe.MatchString(label) {
		return SSHKey{}, fmt.Errorf("%w: label", ErrInvalid)
	}
	k := SSHKey{Domain: domain, Label: label, Fingerprint: ssh.FingerprintSHA256(pub),
		PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub))), AddedAt: s.now().UTC()}
	res, err := s.st.DB().Exec(`INSERT INTO ssh_keys(domain,label,fingerprint,public_key,added_at) VALUES(?,?,?,?,?)`,
		k.Domain, k.Label, k.Fingerprint, k.PublicKey, k.AddedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return k, fmt.Errorf("%w: this key is already added", ErrExists)
		}
		return k, err
	}
	k.ID, _ = res.LastInsertId()
	if err := s.writeAuthorized(x); err != nil {
		_, _ = s.st.DB().Exec(`DELETE FROM ssh_keys WHERE id=?`, k.ID)
		return k, err
	}
	return k, nil
}

// GenerateKey creates an ed25519 pair, stores the public half and returns the private half once.
func (s *Service) GenerateKey(domain, label string) (SSHKey, string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return SSHKey{}, "", err
	}
	sp, err := ssh.NewPublicKey(pub)
	if err != nil {
		return SSHKey{}, "", err
	}
	blk, err := ssh.MarshalPrivateKey(&priv, "hostingpanel:"+domain)
	if err != nil {
		return SSHKey{}, "", err
	}
	k, err := s.AddKey(domain, label, string(ssh.MarshalAuthorizedKey(sp)))
	if err != nil {
		return k, "", err
	}
	return k, string(pem.EncodeToMemory(blk)), nil
}

func (s *Service) RemoveKey(domain string, id int64) error {
	x, err := s.st.Get(domain)
	if err != nil {
		return err
	}
	res, err := s.st.DB().Exec(`DELETE FROM ssh_keys WHERE id=? AND domain=?`, id, domain)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return s.writeAuthorized(x)
}

func (s *Service) writeAuthorized(x storeSite) error {
	keys, err := s.ListKeys(x.Domain)
	if err != nil {
		return err
	}
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k.PublicKey + "\n")
	}
	dir := filepath.Join(s.p.SitesDir, x.User, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(f, []byte(b.String()), 0o600); err != nil {
		return err
	}
	_ = s.run.Run("chown", "-R", x.User+":"+x.User, dir)
	return nil
}

// ---- cron ----

type CronJob struct {
	ID       int64  `json:"id"`
	Domain   string `json:"domain"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Enabled  bool   `json:"enabled"`
}

var (
	scheduleRe = regexp.MustCompile(`^(@(hourly|daily|weekly|monthly|yearly|reboot)|([0-9*/,\-]+ ){4}[0-9*/,\-A-Za-z]+)$`)
)

func (s *Service) ListCron(domain string) ([]CronJob, error) {
	if _, err := s.st.Get(domain); err != nil {
		return nil, err
	}
	rows, err := s.st.DB().Query(`SELECT id,domain,schedule,command,enabled FROM cron_jobs WHERE domain=? ORDER BY id`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CronJob{}
	for rows.Next() {
		var j CronJob
		var en int
		if err := rows.Scan(&j.ID, &j.Domain, &j.Schedule, &j.Command, &en); err != nil {
			return nil, err
		}
		j.Enabled = en == 1
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Service) AddCron(domain, schedule, command string) (CronJob, error) {
	x, err := s.st.Get(domain)
	if err != nil {
		return CronJob{}, err
	}
	schedule, command = strings.TrimSpace(schedule), strings.TrimSpace(command)
	if !scheduleRe.MatchString(schedule) || command == "" || strings.ContainsAny(command, "\r\n\x00") || len(command) > 1000 {
		return CronJob{}, fmt.Errorf("%w: schedule or command (one line, standard 5-field cron)", ErrInvalid)
	}
	res, err := s.st.DB().Exec(`INSERT INTO cron_jobs(domain,schedule,command) VALUES(?,?,?)`, domain, schedule, command)
	if err != nil {
		return CronJob{}, err
	}
	id, _ := res.LastInsertId()
	if err := s.installCron(x); err != nil {
		_, _ = s.st.DB().Exec(`DELETE FROM cron_jobs WHERE id=?`, id)
		return CronJob{}, err
	}
	return CronJob{ID: id, Domain: domain, Schedule: schedule, Command: command, Enabled: true}, nil
}

func (s *Service) RemoveCron(domain string, id int64) error {
	x, err := s.st.Get(domain)
	if err != nil {
		return err
	}
	res, err := s.st.DB().Exec(`DELETE FROM cron_jobs WHERE id=? AND domain=?`, id, domain)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return s.installCron(x)
}

// installCron writes the Site User's crontab. A disabled Site gets an empty crontab (jobs paused, not lost).
func (s *Service) installCron(x storeSite) error {
	var b strings.Builder
	b.WriteString("# managed by hostingpanel - do not edit\n")
	if x.Status == "enabled" {
		jobs, err := s.ListCron(x.Domain)
		if err != nil {
			return err
		}
		for _, j := range jobs {
			if j.Enabled {
				b.WriteString(j.Schedule + " " + j.Command + "\n")
			}
		}
	}
	return s.run.RunIn(b.String(), "crontab", "-u", x.User, "-")
}

// ---- logs ----

var logFiles = map[string]string{"access": "access.log", "error": "error.log", "php": "php-error.log", "apache": "apache-error.log"}

// ReadLog returns the last n lines (max 1000) of a Site log.
func (s *Service) ReadLog(domain, kind string, n int) (string, error) {
	x, err := s.st.Get(domain)
	if err != nil {
		return "", err
	}
	name, ok := logFiles[kind]
	if !ok {
		return "", fmt.Errorf("%w: log name", ErrInvalid)
	}
	if n <= 0 || n > 1000 {
		n = 200
	}
	f, err := os.Open(filepath.Join(s.p.LogDir, x.Domain, name))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, _ := f.Stat()
	const window = 512 * 1024
	if st.Size() > window {
		_, _ = f.Seek(st.Size()-window, 0)
	}
	ring := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		if len(ring) == n {
			ring = ring[1:]
		}
		ring = append(ring, sc.Text())
	}
	return strings.Join(ring, "\n"), nil
}
