package backup

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/system"
	"hostingpanel/agent/internal/tasks"
)

var (
	nameRe   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	labelRe  = regexp.MustCompile(`^[\p{L}\p{N} ._-]{1,60}$`)
	hostRe   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,253}$`)
	userRe   = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
	borgRepo = regexp.MustCompile(`^(/[A-Za-z0-9._/-]+|ssh://[a-z_][a-z0-9_-]*@[A-Za-z0-9._-]+(:[0-9]{1,5})?/[A-Za-z0-9._/~-]+)$`)
)

type Manager struct {
	db       *db
	run      system.Runner
	tasks    *tasks.Manager
	stateDir string
	// SitesBase: file backups are limited to directories under it.
	SitesBase string
	// Notify reports failures to the Notifier (best effort).
	Notify func(level, title, msg string)
	now    func() time.Time
	mu     sync.Mutex
	active map[int64]bool
	fired  map[int64]time.Time // minute each job last fired, so one minute never runs a job twice
}

func New(st *store.Store, run system.Runner, t *tasks.Manager, stateDir string) (*Manager, error) {
	d, err := newDB(st)
	if err != nil {
		return nil, err
	}
	for _, sub := range []string{"staging", "secrets", "tmp"} {
		if err := os.MkdirAll(filepath.Join(stateDir, sub), 0o700); err != nil {
			return nil, err
		}
	}
	return &Manager{db: d, run: run, tasks: t, stateDir: stateDir, SitesBase: "/srv/sites", now: time.Now,
		Notify: func(string, string, string) {}, active: map[int64]bool{}, fired: map[int64]time.Time{}}, nil
}

// ---- destinations ----

type DestinationInput struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Config map[string]string `json:"config"`
	// Secret material, never returned: borg {"passphrase","key"}, sftp {"key"}.
	Passphrase string `json:"passphrase"`
	Key        string `json:"key"`
}

func (m *Manager) AddDestination(in DestinationInput) (Destination, error) {
	if !labelRe.MatchString(in.Name) {
		return Destination{}, fmt.Errorf("%w: name", ErrInvalid)
	}
	cfg := map[string]string{}
	sec := map[string]string{}
	switch in.Type {
	case "local":
		p := filepath.Clean(in.Config["path"])
		if !filepath.IsAbs(p) || p == "/" {
			return Destination{}, fmt.Errorf("%w: path must be an absolute directory", ErrInvalid)
		}
		cfg["path"] = p
	case "sftp":
		port := in.Config["port"]
		if port == "" {
			port = "22"
		}
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 || !hostRe.MatchString(in.Config["host"]) ||
			!userRe.MatchString(in.Config["user"]) || !strings.HasPrefix(in.Config["path"], "/") || in.Key == "" {
			return Destination{}, fmt.Errorf("%w: host, port, user, absolute path and a private key are required", ErrInvalid)
		}
		cfg = map[string]string{"host": in.Config["host"], "port": port, "user": in.Config["user"], "path": filepath.Clean(in.Config["path"])}
		sec["key"] = in.Key
	case "borg":
		if !borgRepo.MatchString(in.Config["repo"]) || len(in.Passphrase) < 8 {
			return Destination{}, fmt.Errorf("%w: repo (local path or ssh://user@host/path) and a passphrase of 8+ characters are required", ErrInvalid)
		}
		cfg["repo"] = in.Config["repo"]
		sec["passphrase"] = in.Passphrase
		if strings.HasPrefix(cfg["repo"], "ssh://") {
			if in.Key == "" {
				return Destination{}, fmt.Errorf("%w: a private key is required for a remote repository", ErrInvalid)
			}
			sec["key"] = in.Key
		}
	default:
		return Destination{}, fmt.Errorf("%w: type", ErrInvalid)
	}
	sj, _ := json.Marshal(sec)
	x := Destination{Name: in.Name, Type: in.Type, Config: cfg}
	secret := ""
	if len(sec) > 0 {
		secret = string(sj)
	}
	id, err := m.db.addDestination(x, secret)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return x, ErrExists
		}
		return x, err
	}
	x.ID, x.HasKey = id, secret != ""
	return x, nil
}

func (m *Manager) Destinations() ([]Destination, error) { return m.db.destinations() }

func (m *Manager) DeleteDestination(id int64) error {
	jobs, _ := m.db.jobs()
	for _, j := range jobs {
		if j.DestinationID == id {
			return fmt.Errorf("%w: used by job %q", ErrInvalid, j.Name)
		}
	}
	_, err := m.db.s.Exec(`DELETE FROM destinations WHERE id=?`, id)
	return err
}

// backend builds the Backend for a Destination. tmp is a scratch dir for archives.
func (m *Manager) backend(id int64) (Backend, error) {
	d, secret, err := m.db.destination(id)
	if err != nil {
		return nil, err
	}
	sec := map[string]string{}
	_ = json.Unmarshal([]byte(secret), &sec)
	tmp := filepath.Join(m.stateDir, "tmp")
	switch d.Type {
	case "local":
		return &tarBackend{t: localTransport{dir: d.Config["path"]}, run: m.run, tmp: tmp}, nil
	case "sftp":
		port, _ := strconv.Atoi(d.Config["port"])
		t := &sftpTransport{host: d.Config["host"], port: port, user: d.Config["user"], dir: d.Config["path"],
			key: sec["key"], hostKey: d.Config["host_key"], pin: func(line string) { m.pinHostKey(id, line) }}
		return &tarBackend{t: t, run: m.run, tmp: tmp}, nil
	default:
		b := &borgBackend{repo: d.Config["repo"], pass: sec["passphrase"], run: m.run}
		if sec["key"] != "" {
			kp := filepath.Join(m.stateDir, "secrets", fmt.Sprintf("borg-%d.key", id))
			if err := os.WriteFile(kp, []byte(strings.TrimSpace(sec["key"])+"\n"), 0o600); err != nil {
				return nil, err
			}
			b.sshKeyPath, b.knownHosts = kp, filepath.Join(m.stateDir, "secrets", "known_hosts")
		}
		return b, nil
	}
}

func (m *Manager) pinHostKey(id int64, line string) {
	d, _, err := m.db.destination(id)
	if err != nil {
		return
	}
	d.Config["host_key"] = line
	cfg, _ := json.Marshal(d.Config)
	_, _ = m.db.s.Exec(`UPDATE destinations SET config=? WHERE id=?`, string(cfg), id)
}

// TestDestination checks reachability and writability; for SFTP it pins the host key on first use.
func (m *Manager) TestDestination(id int64) error {
	b, err := m.backend(id)
	if err != nil {
		return err
	}
	return b.Test()
}

// ---- jobs ----

func (m *Manager) validHome(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	c := filepath.Clean(p)
	base := filepath.Clean(m.SitesBase)
	if !strings.HasPrefix(c, base+"/") || c == base {
		return "", fmt.Errorf("%w: files must be inside %s", ErrInvalid, base)
	}
	return c, nil
}

func (m *Manager) AddJob(j Job) (Job, error) {
	home, err := m.validHome(j.HomePath)
	if err != nil {
		return j, err
	}
	j.HomePath = home
	if !labelRe.MatchString(j.Name) || j.Keep < 1 || j.Keep > 365 {
		return j, fmt.Errorf("%w: name or keep (1-365)", ErrInvalid)
	}
	if j.Schedule != "" {
		if _, err := Due(j.Schedule, time.Now()); err != nil {
			return j, fmt.Errorf("%w: schedule: %v", ErrInvalid, err)
		}
	}
	if j.HomePath == "" && len(j.Databases) == 0 {
		return j, fmt.Errorf("%w: choose files, databases or both", ErrInvalid)
	}
	for _, d := range j.Databases {
		if !nameRe.MatchString(d.Name) || (d.Engine != "mariadb" && d.Engine != "mysql" && d.Engine != "postgresql") {
			return j, fmt.Errorf("%w: database", ErrInvalid)
		}
	}
	if _, _, err := m.db.destination(j.DestinationID); err != nil {
		return j, fmt.Errorf("%w: destination", ErrInvalid)
	}
	id, err := m.db.addJob(j)
	j.ID, j.Enabled = id, true
	return j, err
}

func (m *Manager) Jobs() ([]Job, error)          { return m.db.jobs() }
func (m *Manager) Runs(job int64) ([]Run, error) { return m.db.runs(job) }

func (m *Manager) DeleteJob(id int64) error {
	_, err := m.db.s.Exec(`DELETE FROM jobs WHERE id=?`, id)
	return err
}

func prefixFor(j Job) string {
	return strings.Trim(regexp.MustCompile(`[^a-z0-9.-]+`).ReplaceAllString(strings.ToLower(j.Site), "-"), "-") + "-j" + strconv.FormatInt(j.ID, 10)
}

// dump writes one database to staging/db-<engine>-<name>.sql.gz.
func (m *Manager) dump(w io.Writer, staging string, d DBRef) error {
	out := filepath.Join(staging, "db-"+d.Engine+"-"+d.Name+".sql.gz")
	var cmd string
	switch d.Engine {
	case "postgresql":
		cmd = `set -o pipefail; runuser -u postgres -- pg_dump "$0" | gzip > "$1"`
	case "mysql":
		cmd = `set -o pipefail; mysqldump --single-transaction --routines --events "$0" | gzip > "$1"`
	default:
		cmd = `set -o pipefail; mariadb-dump --single-transaction --routines --events "$0" | gzip > "$1"`
	}
	return m.run.RunW(w, "bash", "-c", cmd, d.Name, out)
}

// RunJob starts a backup of one Job as a Task. A job can have only one run at a time.
func (m *Manager) RunJob(id int64) (store.Task, error) {
	j, err := m.db.job(id)
	if err != nil {
		return store.Task{}, err
	}
	m.mu.Lock()
	if m.active[id] {
		m.mu.Unlock()
		return store.Task{}, fmt.Errorf("%w: this job is already running", ErrExists)
	}
	m.active[id] = true
	m.mu.Unlock()
	return m.tasks.Start("backup.run", "Backup "+j.Name, func(l *tasks.Log) error {
		defer func() { m.mu.Lock(); delete(m.active, id); m.mu.Unlock() }()
		err := m.runJob(l, j)
		if err != nil {
			m.Notify("error", "Backup failed: "+j.Name, err.Error())
		}
		return err
	})
}

func (m *Manager) runJob(l *tasks.Log, j Job) error {
	runID, err := m.db.startRun(j.ID)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	w := io.MultiWriter(l, &buf)
	archive, size, err := m.doBackup(w, j)
	status := "ok"
	if err != nil {
		status = "failed"
		fmt.Fprintf(&buf, "ERROR: %v\n", err)
	}
	m.db.finishRun(runID, status, archive, size, buf.String())
	m.db.touchJob(j.ID, m.now())
	return err
}

func (m *Manager) doBackup(w io.Writer, j Job) (string, int64, error) {
	be, err := m.backend(j.DestinationID)
	if err != nil {
		return "", 0, err
	}
	spec := Spec{Prefix: prefixFor(j)}
	if j.HomePath != "" {
		spec.HomeRel = strings.TrimPrefix(j.HomePath, "/")
	}
	if len(j.Databases) > 0 {
		stage, err := os.MkdirTemp(filepath.Join(m.stateDir, "staging"), "run-")
		if err != nil {
			return "", 0, err
		}
		defer os.RemoveAll(stage)
		for _, d := range j.Databases {
			fmt.Fprintf(w, "dumping %s/%s\n", d.Engine, d.Name)
			if err := m.dump(w, stage, d); err != nil {
				return "", 0, err
			}
		}
		spec.Staging = stage
	}
	archive, size, err := be.Backup(w, spec, m.now())
	if err != nil {
		return "", 0, err
	}
	fmt.Fprintf(w, "created %s\n", archive)
	if gone, err := be.Prune(w, spec.Prefix, j.Keep); err != nil {
		fmt.Fprintf(w, "warning: prune failed: %v\n", err) // the backup itself succeeded
	} else {
		m.db.markPruned(j.ID, gone)
	}
	return archive, size, nil
}

// ---- restore ----

// RestoreInput chooses how a backup comes back.
type RestoreInput struct {
	Mode      string `json:"mode"`      // over | beside
	Databases bool   `json:"databases"` // also restore database dumps
	Files     bool   `json:"files"`
}

func (m *Manager) Restore(runID int64, in RestoreInput) (store.Task, error) {
	r, err := m.db.run(runID)
	if err != nil {
		return store.Task{}, err
	}
	if r.Status != "ok" || r.Archive == "" {
		return store.Task{}, fmt.Errorf("%w: this backup is not available", ErrInvalid)
	}
	if in.Mode != "over" && in.Mode != "beside" {
		return store.Task{}, fmt.Errorf("%w: mode", ErrInvalid)
	}
	if !in.Files && !in.Databases {
		return store.Task{}, fmt.Errorf("%w: choose what to restore (files, databases or both)", ErrInvalid)
	}
	if in.Databases && in.Mode == "beside" {
		return store.Task{}, fmt.Errorf("%w: databases are restored in place only; choose \"over\"", ErrInvalid)
	}
	j, err := m.db.job(r.JobID)
	if err != nil {
		return store.Task{}, err
	}
	return m.tasks.Start("backup.restore", "Restore "+j.Name+" ("+r.Archive+")", func(l *tasks.Log) error {
		err := m.restore(l, j, r, in)
		if err != nil {
			m.Notify("error", "Restore failed: "+j.Name, err.Error())
		}
		return err
	})
}

func (m *Manager) restore(w io.Writer, j Job, r Run, in RestoreInput) error {
	be, err := m.backend(j.DestinationID)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp(filepath.Join(m.stateDir, "staging"), "restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	fmt.Fprintf(w, "fetching %s\n", r.Archive)
	if err := be.Fetch(w, r.Archive, dir); err != nil {
		return err
	}
	if in.Files && j.HomePath != "" {
		src := filepath.Join(dir, strings.TrimPrefix(j.HomePath, "/"))
		target := j.HomePath
		if in.Mode == "beside" {
			target = filepath.Join(j.HomePath, "restore-"+m.now().UTC().Format("20060102-150405"))
		}
		if _, err := os.Stat(src); err != nil {
			return fmt.Errorf("the backup contains no site files")
		}
		if err := os.MkdirAll(target, 0o750); err != nil {
			return err
		}
		fmt.Fprintf(w, "restoring files to %s\n", target)
		if err := m.run.RunW(w, "cp", "-a", src+"/.", target); err != nil {
			return err
		}
	}
	if in.Databases {
		for _, d := range j.Databases {
			dump := filepath.Join(dir, "db-"+d.Engine+"-"+d.Name+".sql.gz")
			if _, err := os.Stat(dump); err != nil {
				continue
			}
			target := d.Name
			if in.Mode == "beside" {
				return fmt.Errorf("databases are restored in place only; choose \"over\" for databases")
			}
			fmt.Fprintf(w, "restoring database %s/%s\n", d.Engine, target)
			if err := m.restoreDB(w, d, dump); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *Manager) restoreDB(w io.Writer, d DBRef, dump string) error {
	var cmd string
	switch d.Engine {
	case "postgresql":
		cmd = `set -o pipefail; gunzip -c "$1" | runuser -u postgres -- psql -v ON_ERROR_STOP=1 "$0"`
	case "mysql":
		cmd = `set -o pipefail; gunzip -c "$1" | mysql "$0"`
	default:
		cmd = `set -o pipefail; gunzip -c "$1" | mariadb "$0"`
	}
	return m.run.RunW(w, "bash", "-c", cmd, d.Name, dump)
}

// ---- scheduler ----

// Tick starts every enabled job whose schedule matches t and that has not run this minute.
func (m *Manager) Tick(t time.Time) {
	jobs, err := m.db.jobs()
	if err != nil {
		return
	}
	for _, j := range jobs {
		if !j.Enabled || j.Schedule == "" {
			continue
		}
		due, err := Due(j.Schedule, t)
		if err != nil || !due {
			continue
		}
		minute := t.UTC().Truncate(time.Minute)
		m.mu.Lock()
		again := m.fired[j.ID].Equal(minute)
		m.fired[j.ID] = minute
		m.mu.Unlock()
		if again {
			continue
		}
		_, _ = m.RunJob(j.ID)
	}
}

// RunScheduler ticks once a minute until stop closes.
func (m *Manager) RunScheduler(stop <-chan struct{}) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			m.Tick(now)
		}
	}
}

// InstallBorg installs the borgbackup package.
func (m *Manager) InstallBorg() (store.Task, error) {
	return m.tasks.Start("borg.install", "Install BorgBackup", func(l *tasks.Log) error {
		return m.run.RunW(l, "apt-get", "-y", "install", "borgbackup")
	})
}
