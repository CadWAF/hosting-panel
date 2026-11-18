// Package dbmanager installs database servers and manages databases and their users.
package dbmanager

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/system"
	"hostingpanel/agent/internal/tasks"
)

var (
	ErrInvalid  = errors.New("invalid input")
	ErrExists   = errors.New("already exists")
	ErrNotFound = errors.New("not found")
	ErrNoServer = errors.New("database server is not installed")

	nameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
)

// Engines and the versions offered for each. One version per engine at a time.
var Engines = map[string][]string{
	"mariadb":    {"10.11", "11.4", "11.8"},
	"mysql":      {"default"},
	"postgresql": {"14", "15", "16", "17"},
}

type Database struct {
	ID        int64     `json:"id"`
	Engine    string    `json:"engine"`
	Name      string    `json:"name"`
	User      string    `json:"user"`
	Site      string    `json:"site"`
	CreatedAt time.Time `json:"created_at"`
}

type EngineInfo struct {
	Engine    string   `json:"engine"`
	Versions  []string `json:"versions"`
	Installed bool     `json:"installed"`
}

type Manager struct {
	run   system.Runner
	tasks *tasks.Manager
	db    *sql.DB
	// Installed reports whether an engine's server is present; replaceable in tests.
	Installed func(engine string) bool
	// Query runs a read-only statement in an engine's admin shell and returns tab-separated rows; replaceable in tests.
	Query func(engine, sql string) (string, error)
	apt   sync.Mutex
}

func New(run system.Runner, t *tasks.Manager, st *store.Store) (*Manager, error) {
	_, err := st.DB().Exec(`CREATE TABLE IF NOT EXISTS dbs(
		id INTEGER PRIMARY KEY AUTOINCREMENT, engine TEXT NOT NULL, name TEXT NOT NULL, user TEXT NOT NULL,
		site TEXT NOT NULL DEFAULT '', created_at TIMESTAMP NOT NULL, UNIQUE(engine,name))`)
	if err != nil {
		return nil, err
	}
	return &Manager{run: run, tasks: t, db: st.DB(), Installed: binaryInstalled}, nil
}

func binaryInstalled(engine string) bool {
	bin := map[string]string{"mariadb": "mariadbd", "mysql": "mysqld", "postgresql": "psql"}[engine]
	if _, err := exec.LookPath(bin); err == nil {
		return true
	}
	// mysqld lives in /usr/sbin, which a non-root PATH may miss.
	for _, d := range []string{"/usr/sbin/", "/usr/bin/"} {
		if _, err := os.Stat(d + bin); err == nil {
			return true
		}
	}
	return false
}

func (m *Manager) Engines() []EngineInfo {
	out := []EngineInfo{}
	for _, e := range []string{"mariadb", "mysql", "postgresql"} {
		out = append(out, EngineInfo{Engine: e, Versions: Engines[e], Installed: m.Installed(e)})
	}
	return out
}

func valid(engine, version string) bool {
	for _, v := range Engines[engine] {
		if v == version {
			return true
		}
	}
	return false
}

// InstallEngine installs a database server as a Task. Only one engine of each kind, one version.
func (m *Manager) InstallEngine(engine, version string) (store.Task, error) {
	if _, ok := Engines[engine]; !ok {
		return store.Task{}, fmt.Errorf("%w: engine", ErrInvalid)
	}
	if !valid(engine, version) {
		return store.Task{}, fmt.Errorf("%w: version", ErrInvalid)
	}
	if m.Installed(engine) {
		return store.Task{}, fmt.Errorf("%w: %s is already installed (one version per engine)", ErrExists, engine)
	}
	title := fmt.Sprintf("Install %s %s", engine, version)
	return m.tasks.Start("db.install", title, func(l *tasks.Log) error {
		m.apt.Lock()
		defer m.apt.Unlock()
		apt := func(a ...string) error {
			return m.run.RunW(l, "apt-get", append([]string{"-y"}, a...)...)
		}
		switch engine {
		case "mariadb":
			script := fmt.Sprintf(`set -e; . /etc/os-release; install -d /etc/apt/keyrings; `+
				`curl -fsSL https://mariadb.org/mariadb_release_signing_key.pgp -o /etc/apt/keyrings/mariadb.pgp; `+
				`printf 'Types: deb\nURIs: https://dlm.mariadb.com/repo/mariadb-server/%s/repo/ubuntu\nSuites: %%s\nComponents: main\nSigned-By: /etc/apt/keyrings/mariadb.pgp\n' "$VERSION_CODENAME" `+
				`> /etc/apt/sources.list.d/mariadb.sources`, version)
			if err := m.run.RunW(l, "sh", "-c", script); err != nil {
				return err
			}
			if err := apt("update"); err != nil {
				return err
			}
			if err := apt("install", "mariadb-server"); err != nil {
				return err
			}
			return m.run.RunW(l, "systemctl", "enable", "--now", "mariadb")
		case "mysql":
			if err := apt("update"); err != nil {
				return err
			}
			if err := apt("install", "mysql-server"); err != nil {
				return err
			}
			return m.run.RunW(l, "systemctl", "enable", "--now", "mysql")
		default:
			if err := apt("install", "postgresql-common"); err != nil {
				return err
			}
			if err := m.run.RunW(l, "/usr/share/postgresql-common/pgdg/apt.postgresql.org.sh", "-y"); err != nil {
				return err
			}
			if err := apt("install", "postgresql-"+version); err != nil {
				return err
			}
			return m.run.RunW(l, "systemctl", "enable", "--now", "postgresql")
		}
	})
}

func newPassword() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// sqlCmd returns how to feed SQL to an engine's admin shell.
func sqlCmd(engine string) (string, []string) {
	switch engine {
	case "postgresql":
		return "runuser", []string{"-u", "postgres", "--", "psql", "-v", "ON_ERROR_STOP=1"}
	case "mysql":
		return "mysql", nil
	}
	return "mariadb", nil
}

func (m *Manager) exec(engine, sqlText string) error {
	name, args := sqlCmd(engine)
	return m.run.RunIn(sqlText, name, args...)
}

func createSQL(engine, name, user, pass string) string {
	if engine == "postgresql" {
		return fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s';\nCREATE DATABASE %s OWNER %s ENCODING 'UTF8';\n", user, pass, name, user)
	}
	return fmt.Sprintf("CREATE DATABASE `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;\n"+
		"CREATE USER '%s'@'localhost' IDENTIFIED BY '%s';\nGRANT ALL PRIVILEGES ON `%s`.* TO '%s'@'localhost';\nFLUSH PRIVILEGES;\n",
		name, user, pass, name, user)
}

// Create makes a database and a same-named user. The password is returned once and never stored.
func (m *Manager) Create(engine, name, site string) (Database, string, error) {
	if _, ok := Engines[engine]; !ok || !nameRe.MatchString(name) {
		return Database{}, "", fmt.Errorf("%w: engine or name (lowercase letters, digits, _; max 32)", ErrInvalid)
	}
	if !m.Installed(engine) {
		return Database{}, "", ErrNoServer
	}
	user := name
	if len(user) > 16 && engine != "postgresql" {
		user = user[:16] // MySQL/MariaDB user length limits
	}
	d := Database{Engine: engine, Name: name, User: user, Site: site, CreatedAt: time.Now().UTC()}
	res, err := m.db.Exec(`INSERT INTO dbs(engine,name,user,site,created_at) VALUES(?,?,?,?,?)`, engine, name, user, site, d.CreatedAt)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return d, "", ErrExists
		}
		return d, "", err
	}
	d.ID, _ = res.LastInsertId()
	pass := newPassword()
	if err := m.exec(engine, createSQL(engine, name, user, pass)); err != nil {
		_, _ = m.db.Exec(`DELETE FROM dbs WHERE id=?`, d.ID)
		return Database{}, "", err
	}
	return d, pass, nil
}

func (m *Manager) List() ([]Database, error) {
	rows, err := m.db.Query(`SELECT id,engine,name,user,site,created_at FROM dbs ORDER BY engine,name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Database{}
	for rows.Next() {
		var d Database
		if err := rows.Scan(&d.ID, &d.Engine, &d.Name, &d.User, &d.Site, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (m *Manager) get(id int64) (Database, error) {
	var d Database
	err := m.db.QueryRow(`SELECT id,engine,name,user,site,created_at FROM dbs WHERE id=?`, id).
		Scan(&d.ID, &d.Engine, &d.Name, &d.User, &d.Site, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

func (m *Manager) ResetPassword(id int64) (string, error) {
	d, err := m.get(id)
	if err != nil {
		return "", err
	}
	pass := newPassword()
	q := fmt.Sprintf("ALTER USER '%s'@'localhost' IDENTIFIED BY '%s';\nFLUSH PRIVILEGES;\n", d.User, pass)
	if d.Engine == "postgresql" {
		q = fmt.Sprintf("ALTER ROLE %s PASSWORD '%s';\n", d.User, pass)
	}
	return pass, m.exec(d.Engine, q)
}

// Delete drops the database and user permanently.
func (m *Manager) Delete(id int64) error {
	d, err := m.get(id)
	if err != nil {
		return err
	}
	q := fmt.Sprintf("DROP DATABASE IF EXISTS `%s`;\nDROP USER IF EXISTS '%s'@'localhost';\n", d.Name, d.User)
	if d.Engine == "postgresql" {
		q = fmt.Sprintf("DROP DATABASE IF EXISTS %s;\nDROP ROLE IF EXISTS %s;\n", d.Name, d.User)
	}
	if err := m.exec(d.Engine, q); err != nil {
		return err
	}
	_, err = m.db.Exec(`DELETE FROM dbs WHERE id=?`, id)
	return err
}
