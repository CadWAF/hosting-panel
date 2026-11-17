// Package backup runs scheduled backups of Sites and Databases to local, SFTP and Borg Destinations.
package backup

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"hostingpanel/agent/internal/store"
)

var (
	ErrInvalid  = errors.New("invalid input")
	ErrNotFound = errors.New("not found")
	ErrExists   = errors.New("already exists")
)

type Destination struct {
	ID     int64             `json:"id"`
	Name   string            `json:"name"`
	Type   string            `json:"type"`   // local | borg | sftp
	Config map[string]string `json:"config"` // path, host, port, user, repo, host_key ...  (secrets are never returned)
	HasKey bool              `json:"has_secret"`
}

type DBRef struct {
	Engine string `json:"engine"`
	Name   string `json:"name"`
}

type Job struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	Site          string    `json:"site"`
	HomePath      string    `json:"home_path"` // site files to include; empty = no files
	Databases     []DBRef   `json:"databases"`
	DestinationID int64     `json:"destination_id"`
	Schedule      string    `json:"schedule"` // 5-field cron; empty = manual only
	Keep          int       `json:"keep"`
	Enabled       bool      `json:"enabled"`
	LastRunAt     time.Time `json:"last_run_at"`
}

type Run struct {
	ID         int64      `json:"id"`
	JobID      int64      `json:"job_id"`
	Status     string     `json:"status"` // running | ok | failed | pruned
	Archive    string     `json:"archive"`
	Size       int64      `json:"size"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Log        string     `json:"log,omitempty"`
}

type db struct{ s *sql.DB }

func newDB(st *store.Store) (*db, error) {
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS destinations(id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE,
			type TEXT NOT NULL, config TEXT NOT NULL, secret TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS jobs(id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, site TEXT NOT NULL,
			home_path TEXT NOT NULL, databases TEXT NOT NULL, destination_id INTEGER NOT NULL, schedule TEXT NOT NULL,
			keep INTEGER NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, last_run_at TIMESTAMP)`,
		`CREATE TABLE IF NOT EXISTS runs(id INTEGER PRIMARY KEY AUTOINCREMENT, job_id INTEGER NOT NULL, status TEXT NOT NULL,
			archive TEXT NOT NULL DEFAULT '', size INTEGER NOT NULL DEFAULT 0, started_at TIMESTAMP NOT NULL,
			finished_at TIMESTAMP, log TEXT NOT NULL DEFAULT '')`,
	} {
		if _, err := st.DB().Exec(q); err != nil {
			return nil, err
		}
	}
	// A previous process may have died mid-run.
	_, _ = st.DB().Exec(`UPDATE runs SET status='failed', log=log||'interrupted by restart', finished_at=? WHERE status='running'`, time.Now().UTC())
	return &db{s: st.DB()}, nil
}

func (d *db) addDestination(x Destination, secret string) (int64, error) {
	cfg, _ := json.Marshal(x.Config)
	r, err := d.s.Exec(`INSERT INTO destinations(name,type,config,secret) VALUES(?,?,?,?)`, x.Name, x.Type, string(cfg), secret)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (d *db) destination(id int64) (Destination, string, error) {
	var x Destination
	var cfg, secret string
	err := d.s.QueryRow(`SELECT id,name,type,config,secret FROM destinations WHERE id=?`, id).Scan(&x.ID, &x.Name, &x.Type, &cfg, &secret)
	if errors.Is(err, sql.ErrNoRows) {
		return x, "", ErrNotFound
	}
	_ = json.Unmarshal([]byte(cfg), &x.Config)
	x.HasKey = secret != ""
	return x, secret, err
}

func (d *db) destinations() ([]Destination, error) {
	rows, err := d.s.Query(`SELECT id FROM destinations ORDER BY name`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		_ = rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	out := []Destination{}
	for _, id := range ids {
		x, _, err := d.destination(id)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, nil
}

func (d *db) addJob(j Job) (int64, error) {
	dbs, _ := json.Marshal(j.Databases)
	r, err := d.s.Exec(`INSERT INTO jobs(name,site,home_path,databases,destination_id,schedule,keep,enabled) VALUES(?,?,?,?,?,?,?,1)`,
		j.Name, j.Site, j.HomePath, string(dbs), j.DestinationID, j.Schedule, j.Keep)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func scanJob(sc interface{ Scan(...any) error }) (Job, error) {
	var j Job
	var dbs string
	var en int
	var last sql.NullTime
	if err := sc.Scan(&j.ID, &j.Name, &j.Site, &j.HomePath, &dbs, &j.DestinationID, &j.Schedule, &j.Keep, &en, &last); err != nil {
		return j, err
	}
	j.Enabled = en == 1
	j.LastRunAt = last.Time
	_ = json.Unmarshal([]byte(dbs), &j.Databases)
	return j, nil
}

const jobCols = `id,name,site,home_path,databases,destination_id,schedule,keep,enabled,last_run_at`

func (d *db) job(id int64) (Job, error) {
	j, err := scanJob(d.s.QueryRow(`SELECT `+jobCols+` FROM jobs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return j, ErrNotFound
	}
	return j, err
}

func (d *db) jobs() ([]Job, error) {
	rows, err := d.s.Query(`SELECT ` + jobCols + ` FROM jobs ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (d *db) touchJob(id int64, t time.Time) {
	_, _ = d.s.Exec(`UPDATE jobs SET last_run_at=? WHERE id=?`, t.UTC(), id)
}

func (d *db) startRun(jobID int64) (int64, error) {
	r, err := d.s.Exec(`INSERT INTO runs(job_id,status,started_at) VALUES(?, 'running', ?)`, jobID, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

func (d *db) finishRun(id int64, status, archive string, size int64, log string) {
	_, _ = d.s.Exec(`UPDATE runs SET status=?, archive=?, size=?, log=?, finished_at=? WHERE id=?`, status, archive, size, log, time.Now().UTC(), id)
}

func (d *db) run(id int64) (Run, error) {
	var r Run
	err := d.s.QueryRow(`SELECT id,job_id,status,archive,size,started_at,finished_at,log FROM runs WHERE id=?`, id).
		Scan(&r.ID, &r.JobID, &r.Status, &r.Archive, &r.Size, &r.StartedAt, &r.FinishedAt, &r.Log)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

func (d *db) runs(jobID int64) ([]Run, error) {
	rows, err := d.s.Query(`SELECT id,job_id,status,archive,size,started_at,finished_at,'' FROM runs WHERE (?=0 OR job_id=?) ORDER BY id DESC LIMIT 100`, jobID, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.JobID, &r.Status, &r.Archive, &r.Size, &r.StartedAt, &r.FinishedAt, &r.Log); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *db) markPruned(jobID int64, archives []string) {
	for _, a := range archives {
		_, _ = d.s.Exec(`UPDATE runs SET status='pruned' WHERE job_id=? AND archive=?`, jobID, a)
	}
}
