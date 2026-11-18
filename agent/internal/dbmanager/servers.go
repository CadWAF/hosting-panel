package dbmanager

import (
	"fmt"
	"strconv"
	"strings"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/tasks"
)

// Stats is the overview of one database server.
type Stats struct {
	Engine    string   `json:"engine"`
	Installed bool     `json:"installed"`
	Running   bool     `json:"running"`
	Version   string   `json:"version"`
	Uptime    int64    `json:"uptime"`      // seconds
	Managed   int      `json:"managed"`     // databases created through the Panel
	Databases []string `json:"databases"`   // every non-system database found on the server
	Size      uint64   `json:"size"`        // bytes on disk (data and indexes)
	Conns     int64    `json:"connections"` // open connections right now
	Queries   uint64   `json:"queries"`     // statements (MariaDB/MySQL) or transactions (PostgreSQL) since start
	Removable bool     `json:"removable"`
	Reason    string   `json:"reason,omitempty"` // why it cannot be removed
}

func (m *Manager) query(engine, sqlText string) (string, error) {
	if m.Query != nil {
		return m.Query(engine, sqlText)
	}
	switch engine {
	case "postgresql":
		return m.run.Output("runuser", "-u", "postgres", "--", "psql", "-X", "-At", "-F", "\t", "-c", sqlText)
	case "mysql":
		return m.run.Output("mysql", "-N", "-B", "-e", sqlText)
	}
	return m.run.Output("mariadb", "-N", "-B", "-e", sqlText)
}

func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// serverDatabases lists the databases on the server itself, so ones created outside the Panel count too.
func (m *Manager) serverDatabases(engine string) ([]string, error) {
	q := "SELECT schema_name FROM information_schema.schemata WHERE schema_name NOT IN ('information_schema','mysql','performance_schema','sys') ORDER BY 1"
	if engine == "postgresql" {
		q = "SELECT datname FROM pg_database WHERE NOT datistemplate AND datname <> 'postgres' ORDER BY 1"
	}
	out, err := m.query(engine, q)
	if err != nil {
		return nil, err
	}
	return lines(out), nil
}

func (m *Manager) managedCount(engine string) int {
	var n int
	_ = m.db.QueryRow(`SELECT COUNT(*) FROM dbs WHERE engine=?`, engine).Scan(&n)
	return n
}

// blocked explains why an engine cannot be removed, or returns "" when it can.
func (m *Manager) blocked(engine string) (reason string, dbs []string) {
	tracked, _ := m.List()
	seen := map[string]bool{}
	for _, d := range tracked {
		if d.Engine == engine {
			seen[d.Name] = true
		}
	}
	real, err := m.serverDatabases(engine)
	if err != nil {
		if len(seen) == 0 {
			return "the server did not answer, so its databases cannot be checked; start it first", nil
		}
	}
	for _, n := range real {
		seen[n] = true
	}
	for n := range seen {
		dbs = append(dbs, n)
	}
	if len(dbs) > 0 {
		return fmt.Sprintf("it still has %d database(s); delete them first", len(dbs)), dbs
	}
	return "", nil
}

// Stats reports version, uptime, size, connections and databases of an installed server.
func (m *Manager) Stats(engine string) (Stats, error) {
	if _, ok := Engines[engine]; !ok {
		return Stats{}, fmt.Errorf("%w: engine", ErrInvalid)
	}
	s := Stats{Engine: engine, Installed: m.Installed(engine), Databases: []string{}, Managed: m.managedCount(engine)}
	if !s.Installed {
		return s, nil
	}
	num := func(q string) string {
		out, err := m.query(engine, q)
		if err != nil {
			return ""
		}
		if l := lines(out); len(l) > 0 {
			return l[0]
		}
		return ""
	}
	if engine == "postgresql" {
		s.Version = num("SELECT version()")
		s.Running = s.Version != ""
		s.Version = strings.Join(strings.Fields(s.Version)[:min(2, len(strings.Fields(s.Version)))], " ")
		s.Uptime, _ = strconv.ParseInt(num("SELECT floor(extract(epoch from now()-pg_postmaster_start_time()))::bigint"), 10, 64)
		s.Size, _ = strconv.ParseUint(num("SELECT COALESCE(sum(pg_database_size(datname)),0)::bigint FROM pg_database"), 10, 64)
		s.Conns, _ = strconv.ParseInt(num("SELECT count(*) FROM pg_stat_activity WHERE datname IS NOT NULL"), 10, 64)
		s.Queries, _ = strconv.ParseUint(num("SELECT COALESCE(sum(xact_commit+xact_rollback),0)::bigint FROM pg_stat_database"), 10, 64)
	} else {
		s.Version = num("SELECT VERSION()")
		s.Running = s.Version != ""
		if out, err := m.query(engine, "SHOW GLOBAL STATUS WHERE Variable_name IN ('Uptime','Threads_connected','Questions')"); err == nil {
			for _, l := range lines(out) {
				k, v, _ := strings.Cut(l, "\t")
				n, _ := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
				switch k {
				case "Uptime":
					s.Uptime = int64(n)
				case "Threads_connected":
					s.Conns = int64(n)
				case "Questions":
					s.Queries = n
				}
			}
		}
		s.Size, _ = strconv.ParseUint(num("SELECT COALESCE(SUM(data_length+index_length),0) FROM information_schema.tables"), 10, 64)
	}
	if s.Running {
		s.Databases, _ = m.serverDatabases(engine)
		if s.Databases == nil {
			s.Databases = []string{}
		}
	}
	s.Reason, _ = m.blocked(engine)
	s.Removable = s.Reason == ""
	return s, nil
}

// packages are the installed Debian packages that make up an engine.
var enginePackages = map[string][]string{
	"mariadb":    {"mariadb-*"},
	"mysql":      {"mysql-server*", "mysql-client*", "mysql-community-*"},
	"postgresql": {"postgresql*"},
}

var engineUnits = map[string][]string{
	"mariadb": {"mariadb"}, "mysql": {"mysql"}, "postgresql": {"postgresql"},
}

// RemoveEngine uninstalls a database server as a Task. It refuses while the server holds any database, whether
// the Panel created it or not, so no data can be lost through this call.
func (m *Manager) RemoveEngine(engine string) (store.Task, error) {
	if _, ok := Engines[engine]; !ok {
		return store.Task{}, fmt.Errorf("%w: engine", ErrInvalid)
	}
	if !m.Installed(engine) {
		return store.Task{}, ErrNoServer
	}
	if reason, dbs := m.blocked(engine); reason != "" {
		msg := fmt.Sprintf("%s cannot be removed: %s", engine, reason)
		if len(dbs) > 0 {
			shown := dbs
			if len(shown) > 5 {
				shown = append(shown[:5:5], "…")
			}
			msg += " (" + strings.Join(shown, ", ") + ")"
		}
		return store.Task{}, fmt.Errorf("%w: %s", ErrExists, msg)
	}
	return m.tasks.Start("db.remove", "Remove "+engine, func(l *tasks.Log) error {
		m.apt.Lock()
		defer m.apt.Unlock()
		if reason, _ := m.blocked(engine); reason != "" { // a database may have appeared while the task waited
			return fmt.Errorf("%s cannot be removed: %s", engine, reason)
		}
		for _, u := range engineUnits[engine] {
			_ = m.run.RunW(l, "systemctl", "disable", "--now", u)
		}
		if engine == "postgresql" {
			if out, _ := m.run.Output("pg_lsclusters", "-h"); out != "" {
				for _, c := range lines(out) {
					if f := strings.Fields(c); len(f) >= 2 {
						_ = m.run.RunW(l, "pg_dropcluster", "--stop", f[0], f[1])
					}
				}
			}
		}
		args := append([]string{"-W", "-f=${Package}\\n"}, enginePackages[engine]...)
		out, _ := m.run.Output("dpkg-query", args...)
		pkgs := lines(out)
		if len(pkgs) == 0 {
			l.Printf("No %s packages are installed", engine)
			return nil
		}
		l.Printf("Removing %s", strings.Join(pkgs, " "))
		if err := m.run.RunW(l, "apt-get", append([]string{"-y", "purge"}, pkgs...)...); err != nil {
			return err
		}
		return m.run.RunW(l, "apt-get", "-y", "autoremove")
	})
}
