// Package store persists the Core Agent's own state in sqlite.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

type Site struct {
	Domain      string            `json:"domain"`
	User        string            `json:"user"`
	PHPVersion  string            `json:"php_version"`
	Status      string            `json:"status"`   // enabled | disabled
	WebMode     string            `json:"web_mode"` // nginx | apache
	ApachePort  int               `json:"apache_port"`
	Preset      string            `json:"preset"` // small | medium | large
	PHPSettings map[string]string `json:"php_settings"`
	TLS         string            `json:"tls"` // "" (off) | apex | www (certificate also covers www.)
	Docroot     string            `json:"docroot"` // folder inside public_html served as the web root ("" = public_html itself)
	CreatedAt   time.Time         `json:"created_at"`
}

const siteCols = `domain,user,php_version,status,web_mode,apache_port,preset,php_settings,tls,docroot,created_at`

func (x *Site) scan(r interface{ Scan(...any) error }) error {
	var js string
	if err := r.Scan(&x.Domain, &x.User, &x.PHPVersion, &x.Status, &x.WebMode, &x.ApachePort, &x.Preset, &js, &x.TLS, &x.Docroot, &x.CreatedAt); err != nil {
		return err
	}
	x.PHPSettings = map[string]string{}
	if js != "" {
		_ = json.Unmarshal([]byte(js), &x.PHPSettings)
	}
	return nil
}

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS sites(
		domain TEXT PRIMARY KEY,
		user TEXT NOT NULL UNIQUE,
		php_version TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL)`)
	if err != nil {
		return nil, err
	}
	for _, alt := range []string{
		`ALTER TABLE sites ADD COLUMN web_mode TEXT NOT NULL DEFAULT 'nginx'`,
		`ALTER TABLE sites ADD COLUMN apache_port INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE sites ADD COLUMN preset TEXT NOT NULL DEFAULT 'medium'`,
		`ALTER TABLE sites ADD COLUMN php_settings TEXT NOT NULL DEFAULT '{}'`,
		`ALTER TABLE sites ADD COLUMN tls TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE sites ADD COLUMN docroot TEXT NOT NULL DEFAULT ''`,
	} {
		_, _ = db.Exec(alt) // fails harmlessly when the column already exists
	}
	st := &Store{db: db}
	if err := st.initTasks(); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle so other Services can keep their own tables in their own file.
func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Insert(site Site) error {
	js, _ := json.Marshal(site.PHPSettings)
	_, err := s.db.Exec(`INSERT INTO sites(`+siteCols+`) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		site.Domain, site.User, site.PHPVersion, site.Status, site.WebMode, site.ApachePort, site.Preset, string(js), site.TLS, site.Docroot, site.CreatedAt)
	return err
}

// Update rewrites every mutable column of an existing site.
func (s *Store) Update(site Site) error {
	js, _ := json.Marshal(site.PHPSettings)
	res, err := s.db.Exec(`UPDATE sites SET php_version=?,status=?,web_mode=?,apache_port=?,preset=?,php_settings=?,tls=?,docroot=? WHERE domain=?`,
		site.PHPVersion, site.Status, site.WebMode, site.ApachePort, site.Preset, string(js), site.TLS, site.Docroot, site.Domain)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Get(domain string) (Site, error) {
	var x Site
	err := x.scan(s.db.QueryRow(`SELECT `+siteCols+` FROM sites WHERE domain=?`, domain))
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

func (s *Store) List() ([]Site, error) {
	rows, err := s.db.Query(`SELECT ` + siteCols + ` FROM sites ORDER BY domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Site{}
	for rows.Next() {
		var x Site
		if err := x.scan(rows); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// UsedApachePorts returns ports already allocated to Sites.
func (s *Store) UsedApachePorts() (map[int]bool, error) {
	l, err := s.List()
	m := map[int]bool{}
	for _, x := range l {
		if x.ApachePort > 0 {
			m[x.ApachePort] = true
		}
	}
	return m, err
}

func (s *Store) SetStatus(domain, status string) error {
	res, err := s.db.Exec(`UPDATE sites SET status=? WHERE domain=?`, status, domain)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Delete(domain string) error {
	_, err := s.db.Exec(`DELETE FROM sites WHERE domain=?`, domain)
	return err
}

func (s *Store) UserTaken(user string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sites WHERE user=?`, user).Scan(&n)
	return n > 0, err
}

// ---- tasks ----

type Task struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Title      string     `json:"title"`
	Status     string     `json:"status"` // running | done | failed | interrupted
	Log        string     `json:"log"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

func (s *Store) initTasks() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS tasks(
		id TEXT PRIMARY KEY, kind TEXT NOT NULL, title TEXT NOT NULL, status TEXT NOT NULL,
		log TEXT NOT NULL DEFAULT '', created_at TIMESTAMP NOT NULL, finished_at TIMESTAMP)`)
	if err != nil {
		return err
	}
	// Anything still "running" belonged to a previous process.
	_, err = s.db.Exec(`UPDATE tasks SET status='interrupted', finished_at=? WHERE status='running'`, time.Now().UTC())
	return err
}

func (s *Store) InsertTask(t Task) error {
	_, err := s.db.Exec(`INSERT INTO tasks(id,kind,title,status,log,created_at) VALUES(?,?,?,?,?,?)`,
		t.ID, t.Kind, t.Title, t.Status, t.Log, t.CreatedAt)
	return err
}

func (s *Store) UpdateTask(id, status, log string, finished *time.Time) error {
	_, err := s.db.Exec(`UPDATE tasks SET status=?, log=?, finished_at=? WHERE id=?`, status, log, finished, id)
	return err
}

func (s *Store) GetTask(id string) (Task, error) {
	var t Task
	err := s.db.QueryRow(`SELECT id,kind,title,status,log,created_at,finished_at FROM tasks WHERE id=?`, id).
		Scan(&t.ID, &t.Kind, &t.Title, &t.Status, &t.Log, &t.CreatedAt, &t.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

func (s *Store) ListTasks(limit int) ([]Task, error) {
	rows, err := s.db.Query(`SELECT id,kind,title,status,'',created_at,finished_at FROM tasks ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		var t Task
		if err := rows.Scan(&t.ID, &t.Kind, &t.Title, &t.Status, &t.Log, &t.CreatedAt, &t.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
