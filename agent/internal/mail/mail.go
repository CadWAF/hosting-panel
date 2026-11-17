// Package mail manages Mail Domains, Mailboxes, DKIM keys, the DNS Checklist and Webmail Login tokens.
// It owns its own state and exports a read-only map database that Postfix and Dovecot query.
package mail

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"

	"hostingpanel/agent/internal/system"
)

var (
	ErrInvalid  = errors.New("invalid input")
	ErrExists   = errors.New("already exists")
	ErrNotFound = errors.New("not found")

	domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	localRe  = regexp.MustCompile(`^[a-z0-9]([a-z0-9._+-]{0,62}[a-z0-9])?$`)

	SSOTTL = 30 * time.Second
)

type Config struct {
	StateDir   string // sqlite state, DKIM keys, export db
	MailRoot   string // /var/mail/vhosts
	Hostname   string // mail host used in MX, e.g. mail.example.com
	ServerIPv4 string
	ServerIPv6 string
	WebmailURL string // https://webmail.example.com
	Selector   string // DKIM selector
}

type Domain struct {
	Name      string    `json:"name"`
	Selector  string    `json:"selector"`
	CreatedAt time.Time `json:"created_at"`
	Mailboxes int       `json:"mailboxes"`
}

type Mailbox struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Domain    string    `json:"domain"`
	CreatedAt time.Time `json:"created_at"`
}

type Alias struct {
	ID          int64  `json:"id"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type Manager struct {
	cfg Config
	run system.Runner
	db  *sql.DB
	now func() time.Time
	mu  sync.Mutex
	// PHPSocket is the PHP-FPM socket serving webmail.
	PHPSocket string
	sso       map[string]ssoEntry // sha256(token) -> entry; in memory on purpose: lost on restart, which only invalidates 30s tokens
}

type ssoEntry struct {
	email   string
	expires time.Time
}

func New(cfg Config, run system.Runner) (*Manager, error) {
	if cfg.Selector == "" {
		cfg.Selector = "mail"
	}
	if err := os.MkdirAll(filepath.Join(cfg.StateDir, "dkim"), 0o750); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(cfg.StateDir, "mail.db")+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS domains(name TEXT PRIMARY KEY, selector TEXT NOT NULL, created_at TIMESTAMP NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS mailboxes(id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL UNIQUE,
			domain TEXT NOT NULL, password TEXT NOT NULL, created_at TIMESTAMP NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS aliases(id INTEGER PRIMARY KEY AUTOINCREMENT, source TEXT NOT NULL UNIQUE, destination TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(q); err != nil {
			return nil, err
		}
	}
	m := &Manager{cfg: cfg, run: run, db: db, now: time.Now, sso: map[string]ssoEntry{}}
	m.loadSettings()
	return m, m.export()
}

func (m *Manager) Close() error { return m.db.Close() }

// export rebuilds the read-only database Postfix/Dovecot use. Rebuilt atomically after every change.
func (m *Manager) export() error {
	tmp := filepath.Join(m.cfg.StateDir, "mailmap.db.new")
	_ = os.Remove(tmp)
	out, err := sql.Open("sqlite", tmp)
	if err != nil {
		return err
	}
	defer out.Close()
	for _, q := range []string{
		`CREATE TABLE domains(name TEXT PRIMARY KEY)`,
		`CREATE TABLE mailboxes(email TEXT PRIMARY KEY, password TEXT NOT NULL, maildir TEXT NOT NULL)`,
		`CREATE TABLE aliases(source TEXT PRIMARY KEY, destination TEXT NOT NULL)`,
	} {
		if _, err := out.Exec(q); err != nil {
			return err
		}
	}
	rows, _ := m.db.Query(`SELECT name FROM domains`)
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		_, _ = out.Exec(`INSERT INTO domains VALUES(?)`, n)
	}
	rows.Close()
	rows, _ = m.db.Query(`SELECT email,password,domain FROM mailboxes`)
	for rows.Next() {
		var e, p, d string
		_ = rows.Scan(&e, &p, &d)
		if !strings.HasPrefix(p, "{") { // a stored "{SCHEME}hash" (imported from another server) is passed through as is
			p = "{BLF-CRYPT}" + p
		}
		_, _ = out.Exec(`INSERT INTO mailboxes VALUES(?,?,?)`, e, p, d+"/"+strings.SplitN(e, "@", 2)[0]+"/")
	}
	rows.Close()
	rows, _ = m.db.Query(`SELECT source,destination FROM aliases`)
	for rows.Next() {
		var s, d string
		_ = rows.Scan(&s, &d)
		_, _ = out.Exec(`INSERT INTO aliases VALUES(?,?)`, s, d)
	}
	rows.Close()
	out.Close()
	_ = os.Chmod(tmp, 0o640)
	return os.Rename(tmp, filepath.Join(m.cfg.StateDir, "mailmap.db"))
}

// ---- domains ----

func newDKIM() (privPEM string, pubB64 string, err error) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	priv := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(k)})
	return string(priv), base64.StdEncoding.EncodeToString(der), nil
}

func (m *Manager) dkimPath(domain, sel string) string {
	return filepath.Join(m.cfg.StateDir, "dkim", domain+"."+sel+".key")
}

// DKIMPublic returns the base64 public key for the DNS record.
func (m *Manager) dkimPublic(domain, sel string) (string, error) {
	b, err := os.ReadFile(m.dkimPath(domain, sel))
	if err != nil {
		return "", err
	}
	blk, _ := pem.Decode(b)
	k, err := x509.ParsePKCS1PrivateKey(blk.Bytes)
	if err != nil {
		return "", err
	}
	der, _ := x509.MarshalPKIXPublicKey(&k.PublicKey)
	return base64.StdEncoding.EncodeToString(der), nil
}

func (m *Manager) AddDomain(name string) (Domain, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !domainRe.MatchString(name) {
		return Domain{}, fmt.Errorf("%w: domain", ErrInvalid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.db.QueryRow(`SELECT name FROM domains WHERE name=?`, name).Scan(new(string)); err == nil {
		return Domain{}, ErrExists
	}
	priv, _, err := newDKIM()
	if err != nil {
		return Domain{}, err
	}
	kp := m.dkimPath(name, m.cfg.Selector)
	if err := os.WriteFile(kp, []byte(priv), 0o640); err != nil {
		return Domain{}, err
	}
	d := Domain{Name: name, Selector: m.cfg.Selector, CreatedAt: m.now().UTC()}
	if _, err := m.db.Exec(`INSERT INTO domains VALUES(?,?,?)`, d.Name, d.Selector, d.CreatedAt); err != nil {
		_ = os.Remove(kp)
		return Domain{}, err
	}
	return d, m.export()
}

func (m *Manager) Domains() ([]Domain, error) {
	rows, err := m.db.Query(`SELECT d.name,d.selector,d.created_at,(SELECT COUNT(*) FROM mailboxes WHERE domain=d.name) FROM domains d ORDER BY d.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Domain{}
	for rows.Next() {
		var d Domain
		if err := rows.Scan(&d.Name, &d.Selector, &d.CreatedAt, &d.Mailboxes); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (m *Manager) DeleteDomain(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int
	_ = m.db.QueryRow(`SELECT COUNT(*) FROM mailboxes WHERE domain=?`, name).Scan(&n)
	if n > 0 {
		return fmt.Errorf("%w: delete its %d mailbox(es) first", ErrInvalid, n)
	}
	res, err := m.db.Exec(`DELETE FROM domains WHERE name=?`, name)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	_, _ = m.db.Exec(`DELETE FROM aliases WHERE source LIKE ?`, "%@"+name)
	_ = os.Remove(m.dkimPath(name, m.cfg.Selector))
	return m.export()
}

// ---- mailboxes ----

func splitEmail(email string) (local, domain string, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	i := strings.LastIndex(email, "@")
	if i < 1 || !localRe.MatchString(email[:i]) || !domainRe.MatchString(email[i+1:]) {
		return "", "", fmt.Errorf("%w: email address", ErrInvalid)
	}
	return email[:i], email[i+1:], nil
}

func checkPassword(p string) error {
	if len(p) < 10 || len(p) > 200 {
		return fmt.Errorf("%w: password must be 10-200 characters", ErrInvalid)
	}
	return nil
}

func (m *Manager) AddMailbox(email, password string) (Mailbox, error) {
	local, domain, err := splitEmail(email)
	if err != nil {
		return Mailbox{}, err
	}
	if err := checkPassword(password); err != nil {
		return Mailbox{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.db.QueryRow(`SELECT name FROM domains WHERE name=?`, domain).Scan(new(string)); err != nil {
		return Mailbox{}, fmt.Errorf("%w: add the mail domain %s first", ErrInvalid, domain)
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return Mailbox{}, err
	}
	mb := Mailbox{Email: local + "@" + domain, Domain: domain, CreatedAt: m.now().UTC()}
	res, err := m.db.Exec(`INSERT INTO mailboxes(email,domain,password,created_at) VALUES(?,?,?,?)`, mb.Email, domain, string(h), mb.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return mb, ErrExists
		}
		return mb, err
	}
	mb.ID, _ = res.LastInsertId()
	if err := m.run.Run("install", "-d", "-o", "vmail", "-g", "vmail", "-m", "0700", filepath.Join(m.cfg.MailRoot, domain, local)); err != nil {
		return mb, err
	}
	return mb, m.export()
}

func (m *Manager) Mailboxes(domain string) ([]Mailbox, error) {
	rows, err := m.db.Query(`SELECT id,email,domain,created_at FROM mailboxes WHERE (?='' OR domain=?) ORDER BY email`, domain, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Mailbox{}
	for rows.Next() {
		var x Mailbox
		if err := rows.Scan(&x.ID, &x.Email, &x.Domain, &x.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (m *Manager) mailbox(id int64) (Mailbox, error) {
	var x Mailbox
	err := m.db.QueryRow(`SELECT id,email,domain,created_at FROM mailboxes WHERE id=?`, id).Scan(&x.ID, &x.Email, &x.Domain, &x.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

func (m *Manager) SetPassword(id int64, password string) error {
	if err := checkPassword(password); err != nil {
		return err
	}
	if _, err := m.mailbox(id); err != nil {
		return err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.db.Exec(`UPDATE mailboxes SET password=? WHERE id=?`, string(h), id); err != nil {
		return err
	}
	return m.export()
}

// DeleteMailbox removes the account; its mail is moved aside, not erased.
func (m *Manager) DeleteMailbox(id int64) error {
	x, err := m.mailbox(id)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.db.Exec(`DELETE FROM mailboxes WHERE id=?`, id); err != nil {
		return err
	}
	_, _ = m.db.Exec(`DELETE FROM aliases WHERE source=?`, x.Email)
	local := strings.SplitN(x.Email, "@", 2)[0]
	src := filepath.Join(m.cfg.MailRoot, x.Domain, local)
	trash := filepath.Join(m.cfg.MailRoot, ".trash")
	_ = m.run.Run("mkdir", "-p", trash)
	_ = m.run.Run("mv", src, filepath.Join(trash, fmt.Sprintf("%s-%s-%d", x.Domain, local, m.now().Unix())))
	return m.export()
}

func (m *Manager) AddAlias(source, destination string) (Alias, error) {
	if _, _, err := splitEmail(destination); err != nil {
		return Alias{}, err
	}
	local, domain, err := splitEmail(source)
	if err != nil {
		return Alias{}, err
	}
	if err := m.db.QueryRow(`SELECT name FROM domains WHERE name=?`, domain).Scan(new(string)); err != nil {
		return Alias{}, fmt.Errorf("%w: add the mail domain %s first", ErrInvalid, domain)
	}
	a := Alias{Source: local + "@" + domain, Destination: strings.ToLower(strings.TrimSpace(destination))}
	m.mu.Lock()
	defer m.mu.Unlock()
	res, err := m.db.Exec(`INSERT INTO aliases(source,destination) VALUES(?,?)`, a.Source, a.Destination)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return a, ErrExists
		}
		return a, err
	}
	a.ID, _ = res.LastInsertId()
	return a, m.export()
}

func (m *Manager) Aliases() ([]Alias, error) {
	rows, err := m.db.Query(`SELECT id,source,destination FROM aliases ORDER BY source`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Alias{}
	for rows.Next() {
		var a Alias
		if err := rows.Scan(&a.ID, &a.Source, &a.Destination); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (m *Manager) DeleteAlias(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.db.Exec(`DELETE FROM aliases WHERE id=?`, id); err != nil {
		return err
	}
	return m.export()
}

// ---- Webmail Login ----

// IssueSSO returns a single-use, 30-second URL that signs the Administrator into one mailbox's webmail.
func (m *Manager) IssueSSO(id int64) (string, error) {
	x, err := m.mailbox(id)
	if err != nil {
		return "", err
	}
	if m.cfg.WebmailURL == "" {
		return "", fmt.Errorf("%w: webmail is not configured", ErrInvalid)
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for k, e := range m.sso { // drop expired
		if now.After(e.expires) {
			delete(m.sso, k)
		}
	}
	m.sso[hashToken(tok)] = ssoEntry{email: x.Email, expires: now.Add(SSOTTL)}
	return strings.TrimRight(m.cfg.WebmailURL, "/") + "/?_task=login&_hp_sso=" + tok, nil
}

func hashToken(t string) string { h := sha256.Sum256([]byte(t)); return hex.EncodeToString(h[:]) }

// ConsumeSSO validates and burns a token, returning the mailbox it was issued for.
func (m *Manager) ConsumeSSO(token string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := hashToken(token)
	e, ok := m.sso[k]
	delete(m.sso, k)
	if !ok || m.now().After(e.expires) {
		return "", ErrNotFound
	}
	return e.email, nil
}

// ---- settings ----

type Settings struct {
	Hostname   string `json:"hostname"`
	ServerIPv4 string `json:"server_ipv4"`
	ServerIPv6 string `json:"server_ipv6"`
	WebmailURL string `json:"webmail_url"`
	PHPSocket  string `json:"php_socket"`
	Installed  bool   `json:"installed"`
}

func (m *Manager) Settings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Settings{Hostname: m.cfg.Hostname, ServerIPv4: m.cfg.ServerIPv4, ServerIPv6: m.cfg.ServerIPv6,
		WebmailURL: m.cfg.WebmailURL, PHPSocket: m.phpSocket(), Installed: m.Installed()}
}

func (m *Manager) phpSocket() string {
	if m.PHPSocket == "" {
		return "/run/php/php-fpm.sock"
	}
	return m.PHPSocket
}

var (
	hostRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)
	urlRe  = regexp.MustCompile(`^https://([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}/?$`)
)

func (m *Manager) SetSettings(s Settings) error {
	s.Hostname = strings.ToLower(strings.TrimSpace(s.Hostname))
	if !hostRe.MatchString(s.Hostname) {
		return fmt.Errorf("%w: hostname", ErrInvalid)
	}
	if s.ServerIPv4 != "" && (net.ParseIP(s.ServerIPv4) == nil || !strings.Contains(s.ServerIPv4, ".")) {
		return fmt.Errorf("%w: IPv4 address", ErrInvalid)
	}
	if s.ServerIPv6 != "" && (net.ParseIP(s.ServerIPv6) == nil || !strings.Contains(s.ServerIPv6, ":")) {
		return fmt.Errorf("%w: IPv6 address", ErrInvalid)
	}
	if !urlRe.MatchString(s.WebmailURL) {
		return fmt.Errorf("%w: webmail URL must be https://host", ErrInvalid)
	}
	if s.PHPSocket != "" && !strings.HasPrefix(s.PHPSocket, "/run/php/") {
		return fmt.Errorf("%w: php socket", ErrInvalid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg.Hostname, m.cfg.ServerIPv4, m.cfg.ServerIPv6, m.cfg.WebmailURL = s.Hostname, s.ServerIPv4, s.ServerIPv6, strings.TrimRight(s.WebmailURL, "/")
	m.PHPSocket = s.PHPSocket
	b, _ := json.Marshal(s)
	return os.WriteFile(filepath.Join(m.cfg.StateDir, "settings.json"), b, 0o600)
}

func (m *Manager) loadSettings() {
	b, err := os.ReadFile(filepath.Join(m.cfg.StateDir, "settings.json"))
	if err != nil {
		return
	}
	var s Settings
	if json.Unmarshal(b, &s) == nil {
		m.cfg.Hostname, m.cfg.ServerIPv4, m.cfg.ServerIPv6, m.cfg.WebmailURL, m.PHPSocket = s.Hostname, s.ServerIPv4, s.ServerIPv6, s.WebmailURL, s.PHPSocket
	}
}
