package sysmgr

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/tasks"
)

// CronJob is a server-level scheduled command, kept in one managed file under /etc/cron.d.
type CronJob struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Schedule string `json:"schedule"`
	User     string `json:"user"`
	Command  string `json:"command"`
	Enabled  bool   `json:"enabled"`
}

const cronFile = "/etc/cron.d/hostingpanel-jobs"

var (
	cronMu     sync.Mutex
	cronFieldR = regexp.MustCompile(`^[0-9*/,\-A-Za-z]{1,40}$`)
	cronMacros = map[string]bool{"@reboot": true, "@hourly": true, "@daily": true, "@weekly": true, "@monthly": true, "@yearly": true}
)

type cronMeta struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

func (m *Manager) loadCron() []CronJob {
	jobs := []CronJob{}
	lines := strings.Split(m.read(cronFile), "\n")
	for i := 0; i < len(lines); i++ {
		meta, ok := strings.CutPrefix(lines[i], "#hp ")
		if !ok || i+1 >= len(lines) {
			continue
		}
		var mt cronMeta
		if json.Unmarshal([]byte(meta), &mt) != nil {
			continue
		}
		i++
		body := strings.TrimPrefix(lines[i], "#")
		var sched, rest string
		if strings.HasPrefix(body, "@") {
			f := strings.SplitN(body, " ", 2)
			sched, rest = f[0], strings.TrimSpace(f[len(f)-1])
		} else {
			f := strings.SplitN(body, " ", 6)
			if len(f) < 6 {
				continue
			}
			sched, rest = strings.Join(f[:5], " "), f[5]
		}
		u, cmd, _ := strings.Cut(strings.TrimSpace(rest), " ")
		jobs = append(jobs, CronJob{ID: mt.ID, Name: mt.Name, Schedule: sched, User: u, Command: strings.TrimSpace(cmd), Enabled: mt.Enabled})
	}
	return jobs
}

func (m *Manager) saveCron(jobs []CronJob) error {
	var b strings.Builder
	b.WriteString("# Managed by Hosting Panel. Edit jobs in the Panel; manual edits are overwritten.\nSHELL=/bin/sh\nPATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin\n\n")
	for _, j := range jobs {
		meta, _ := json.Marshal(cronMeta{ID: j.ID, Name: j.Name, Enabled: j.Enabled})
		fmt.Fprintf(&b, "#hp %s\n", meta)
		prefix := ""
		if !j.Enabled {
			prefix = "#"
		}
		fmt.Fprintf(&b, "%s%s %s %s\n", prefix, j.Schedule, j.User, j.Command)
	}
	return m.write(cronFile, b.String(), 0o644)
}

func (m *Manager) CronJobs() []CronJob { cronMu.Lock(); defer cronMu.Unlock(); return m.loadCron() }

func (m *Manager) validateJob(j CronJob) (CronJob, error) {
	j.Name = strings.TrimSpace(j.Name)
	j.Command = strings.TrimSpace(j.Command)
	j.Schedule = strings.Join(strings.Fields(j.Schedule), " ")
	if j.Name == "" || len(j.Name) > 80 || strings.ContainsAny(j.Name, "\r\n") {
		return j, invalid("name (up to 80 characters)")
	}
	if j.Command == "" || len(j.Command) > 1000 || strings.ContainsAny(j.Command, "\r\n\x00") {
		return j, invalid("command must be one line")
	}
	if j.User == "" {
		j.User = "root"
	}
	if !nameRe.MatchString(j.User) {
		return j, invalid("user")
	}
	if j.User != "root" {
		if _, _, _, err := m.Lookup(j.User); err != nil {
			return j, invalid("user %q does not exist", j.User)
		}
	}
	if !cronMacros[j.Schedule] {
		f := strings.Fields(j.Schedule)
		if len(f) != 5 {
			return j, invalid("schedule needs 5 fields (minute hour day month weekday) or a macro like @daily")
		}
		for _, x := range f {
			if !cronFieldR.MatchString(x) {
				return j, invalid("schedule field %q", x)
			}
		}
	}
	return j, nil
}

func (m *Manager) AddCron(j CronJob) (CronJob, error) {
	j, err := m.validateJob(j)
	if err != nil {
		return j, err
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	j.ID, j.Enabled = hex.EncodeToString(b), true
	cronMu.Lock()
	defer cronMu.Unlock()
	return j, m.saveCron(append(m.loadCron(), j))
}

func (m *Manager) UpdateCron(id string, in CronJob) error {
	in, err := m.validateJob(in)
	if err != nil {
		return err
	}
	cronMu.Lock()
	defer cronMu.Unlock()
	jobs := m.loadCron()
	for i := range jobs {
		if jobs[i].ID == id {
			in.ID = id
			jobs[i] = in
			return m.saveCron(jobs)
		}
	}
	return ErrNotFound
}

func (m *Manager) DeleteCron(id string) error {
	cronMu.Lock()
	defer cronMu.Unlock()
	jobs := m.loadCron()
	for i := range jobs {
		if jobs[i].ID == id {
			return m.saveCron(append(jobs[:i], jobs[i+1:]...))
		}
	}
	return ErrNotFound
}

// RunCron runs a job once now, as its user, and returns the Task whose log shows the output.
func (m *Manager) RunCron(id string) (store.Task, error) {
	var job *CronJob
	for _, j := range m.CronJobs() {
		if j.ID == id {
			j := j
			job = &j
		}
	}
	if job == nil {
		return store.Task{}, ErrNotFound
	}
	return m.Tasks.Start("cron.run", "Run "+job.Name, func(l *tasks.Log) error {
		l.Printf("$ %s", job.Command)
		if job.User == "root" {
			return m.Run.RunW(l, "/bin/sh", "-c", job.Command)
		}
		return m.Run.RunW(l, "runuser", "-u", job.User, "--", "/bin/sh", "-c", job.Command)
	})
}
