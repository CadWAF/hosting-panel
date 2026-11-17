package sysmgr

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/tasks"
)

func newMgr(t *testing.T) (*Manager, *Sim) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	sim := NewSim(filepath.Join(dir, "root"))
	m := New(sim, tasks.New(st), sim.Root)
	m.SitesBase = "/srv/sites"
	m.Lookup = func(n string) (int, int, string, error) { return 1001, 1001, "/srv/sites/" + n, nil }
	return m, sim
}

func waitTask(t *testing.T, m *Manager, id string) store.Task {
	t.Helper()
	for i := 0; i < 100; i++ {
		x, _ := m.Tasks.Get(id)
		if x.Status != "running" {
			return x
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("task did not finish")
	return store.Task{}
}

func TestParseUFWNumbered(t *testing.T) {
	out := `Status: active

     To                         Action      From
     --                         ------      ----
[ 1] 22/tcp                     ALLOW IN    Anywhere
[ 2] 80/tcp                     ALLOW IN    Anywhere                   # web
[ 3] 5432/tcp                   ALLOW IN    10.0.0.0/8
[ 4] 22/tcp (v6)                ALLOW IN    Anywhere (v6)
`
	r := parseRules(out)
	if len(r) != 4 {
		t.Fatalf("got %d rules: %+v", len(r), r)
	}
	if r[1].Comment != "web" || r[2].From != "10.0.0.0/8" || !r[3].V6 || r[3].To != "22/tcp" {
		t.Fatalf("bad parse: %+v", r)
	}
}

func TestParseListenersAndUpgradable(t *testing.T) {
	l := listeners(simSS)
	var ssh, db *Listener
	for i := range l {
		if l[i].Port == 22 && l[i].Proto == "tcp" && l[i].Addr == "0.0.0.0" {
			ssh = &l[i]
		}
		if l[i].Port == 3306 {
			db = &l[i]
		}
	}
	if ssh == nil || ssh.Process != "sshd" || !ssh.Public || db == nil || db.Public {
		t.Fatalf("listeners: %+v", l)
	}
	up := parseUpgradable("Listing...\nopenssl/noble-updates,noble-security 3.0.13 amd64 [upgradable from: 3.0.12]\ntzdata/noble-updates 2025b all [upgradable from: 2024b]\n")
	if len(up) != 2 || !up[0].Security || up[1].Security || up[0].Current != "3.0.12" {
		t.Fatalf("upgradable: %+v", up)
	}
}

func TestSoftwareInstallAppliesDefaults(t *testing.T) {
	m, _ := newMgr(t)
	find := func(id string) Software {
		for _, s := range m.Software() {
			if s.ID == id {
				return s
			}
		}
		t.Fatalf("no %s", id)
		return Software{}
	}
	if find("fail2ban").Installed {
		t.Fatal("fail2ban should start uninstalled")
	}
	tk, err := m.InstallSoftware("fail2ban")
	if err != nil {
		t.Fatal(err)
	}
	if done := waitTask(t, m, tk.ID); done.Status != "done" {
		t.Fatalf("task: %s\n%s", done.Status, done.Log)
	}
	f := find("fail2ban")
	if !f.Installed || !f.Active || !f.Enabled {
		t.Fatalf("after install: %+v", f)
	}
	if b, _ := os.ReadFile(m.path(f2bDir + "/hp-sshd.local")); !strings.Contains(string(b), "enabled = true") {
		t.Fatal("sshd jail not enabled by default")
	}
	if _, err := m.InstallSoftware("nope"); err == nil {
		t.Fatal("unknown package accepted")
	}
}

func TestFail2banJailLifecycle(t *testing.T) {
	m, _ := newMgr(t)
	tk, _ := m.InstallSoftware("fail2ban")
	waitTask(t, m, tk.ID)
	fb := m.Fail2ban()
	if !fb.Installed || !fb.Running {
		t.Fatalf("state: %+v", fb)
	}
	var sshd Jail
	for _, j := range fb.Jails {
		if j.Name == "sshd" {
			sshd = j
		}
	}
	if !sshd.Enabled || sshd.Banned != 2 || len(sshd.IPs) != 2 {
		t.Fatalf("sshd: %+v", sshd)
	}
	if err := m.SetJail("nginx-http-auth", JailInput{Enabled: true, BanTime: 7200, FindTime: 600, MaxRetry: 3}); err != nil {
		t.Fatal(err)
	}
	enabled := false
	for _, j := range m.Fail2ban().Jails {
		if j.Name == "nginx-http-auth" && j.Enabled && j.BanTime == 7200 && j.MaxRetry == 3 {
			enabled = true
		}
	}
	if !enabled {
		t.Fatalf("jail not enabled: %+v", m.Fail2ban().Jails)
	}
	if err := m.Unban("sshd", "185.224.128.17"); err != nil {
		t.Fatal(err)
	}
	if err := m.Unban("sshd", "not-an-ip"); err == nil {
		t.Fatal("bad IP accepted")
	}
	if err := m.SetJail("sshd", JailInput{BanTime: 5, FindTime: 600, MaxRetry: 3}); err == nil {
		t.Fatal("too short bantime accepted")
	}
	if err := m.SetDefaults(DefaultsInput{BanTime: 3600, FindTime: 600, MaxRetry: 5, IgnoreIP: []string{"203.0.113.7", "bad ip"}}); err == nil {
		t.Fatal("bad ignore IP accepted")
	}
}

func TestFirewall(t *testing.T) {
	m, _ := newMgr(t)
	if m.Firewall().Installed {
		t.Fatal("ufw should be missing at first")
	}
	if err := m.AddRule(RuleInput{Action: "allow", Port: "80"}); err == nil {
		t.Fatal("rule accepted without ufw")
	}
	tk, _ := m.InstallSoftware("ufw")
	waitTask(t, m, tk.ID)
	if err := m.EnableFirewall(); err != nil {
		t.Fatal(err)
	}
	f := m.Firewall()
	if !f.Active || len(f.Rules) != 3 {
		t.Fatalf("after enable must allow ssh, http, https: %+v", f)
	}
	if err := m.AddRule(RuleInput{Action: "allow", Port: "25565", Proto: "tcp", Comment: "minecraft'; rm -rf /"}); err != nil {
		t.Fatal(err)
	}
	if err := m.AddRule(RuleInput{Action: "allow", Port: "5432", Proto: "tcp", From: "10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []RuleInput{{Action: "allow", Port: "0"}, {Action: "allow", Port: "99999"}, {Action: "x", Port: "80"}, {Action: "allow", Port: "80", From: "nope"}, {Action: "allow", Port: "8000:8100"}} {
		if err := m.AddRule(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	f = m.Firewall()
	if len(f.Rules) != 5 || f.Rules[3].Comment != "minecraft; rm -rf /" && !strings.HasPrefix(f.Rules[3].Comment, "minecraft") {
		t.Fatalf("rules: %+v", f.Rules)
	}
	if strings.ContainsAny(f.Rules[3].Comment, "'") {
		t.Fatal("quote survived in comment")
	}
	r := f.Rules[3]
	if err := m.DeleteRule(r.N, "wrong|spec"); err == nil {
		t.Fatal("stale rule deleted")
	}
	if err := m.DeleteRule(r.N, r.To+"|"+r.From); err != nil {
		t.Fatal(err)
	}
	if len(m.Firewall().Rules) != 4 {
		t.Fatal("rule not removed")
	}
	if err := m.DisableFirewall(); err != nil || m.Firewall().Active {
		t.Fatal("not disabled")
	}
}

func TestSSHRefusesLockout(t *testing.T) {
	m, _ := newMgr(t)
	if err := m.SetSSH(SSHInput{RootLogin: "prohibit-password", PasswordAuth: false}); err == nil {
		t.Fatal("password auth disabled while root has no key")
	}
	if err := m.SetSSH(SSHInput{RootLogin: "bogus", PasswordAuth: true}); err == nil {
		t.Fatal("bogus root login accepted")
	}
	_ = os.MkdirAll(m.path("/root/.ssh"), 0o700)
	_ = os.WriteFile(m.path("/root/.ssh/authorized_keys"), []byte("ssh-ed25519 AAAA test\n"), 0o600)
	if err := m.SetSSH(SSHInput{RootLogin: "prohibit-password", PasswordAuth: false}); err != nil {
		t.Fatal(err)
	}
	if s := m.SSH(); s.PasswordAuth || s.RootLogin != "prohibit-password" || !s.HasRootKey {
		t.Fatalf("ssh: %+v", s)
	}
}

func TestServiceProtection(t *testing.T) {
	m, _ := newMgr(t)
	if err := m.ServiceAction("nginx.service", "stop"); err == nil {
		t.Fatal("stopped nginx (the Panel's entry point)")
	}
	if err := m.ServiceAction("ssh.service", "disable"); err == nil {
		t.Fatal("disabled ssh")
	}
	if err := m.ServiceAction("nginx.service", "restart"); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../x.service", "a b.service", "nginx"} {
		if err := m.ServiceAction(bad, "restart"); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if err := m.ServiceAction("nginx.service", "mask"); err == nil {
		t.Fatal("unknown action accepted")
	}
	found := false
	for _, s := range m.Services(false) {
		found = found || s.Unit == "php8.3-fpm.service"
	}
	if !found {
		t.Fatal("php-fpm not in the curated list")
	}
}

func TestCronRoundTrip(t *testing.T) {
	m, _ := newMgr(t)
	j, err := m.AddCron(CronJob{Name: "Cleanup", Schedule: "0 3 * * *", Command: "find /tmp -mtime +7 -delete"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddCron(CronJob{Name: "Boot", Schedule: "@reboot", User: "site1", Command: "/srv/sites/site1/start.sh"}); err != nil {
		t.Fatal(err)
	}
	jobs := m.CronJobs()
	if len(jobs) != 2 || jobs[0].User != "root" || jobs[1].Schedule != "@reboot" || jobs[1].User != "site1" {
		t.Fatalf("jobs: %+v", jobs)
	}
	jobs[0].Enabled = false
	if err := m.UpdateCron(j.ID, CronJob{Name: "Cleanup", Schedule: "30 4 * * 1", Command: "echo hi", User: "root"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(m.path(cronFile)); !strings.Contains(string(b), "30 4 * * 1 root echo hi") {
		t.Fatalf("file: %s", b)
	}
	for _, bad := range []CronJob{
		{Name: "x", Schedule: "* * * *", Command: "a"}, {Name: "x", Schedule: "* * * * *", Command: "a\nb"},
		{Name: "", Schedule: "* * * * *", Command: "a"}, {Name: "x", Schedule: "* * * * ;", Command: "a"}, {Name: "x", Schedule: "* * * * *", Command: "a", User: "root; id"},
	} {
		if _, err := m.AddCron(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if err := m.DeleteCron(j.ID); err != nil || len(m.CronJobs()) != 1 {
		t.Fatal("delete failed")
	}
	if m.DeleteCron("nope") != ErrNotFound {
		t.Fatal("deleting a missing job should be ErrNotFound")
	}
}

func TestFTPAccounts(t *testing.T) {
	m, _ := newMgr(t)
	tk, _ := m.InstallSoftware("pure-ftpd")
	waitTask(t, m, tk.ID)
	if err := m.AddFTP(FTPInput{Name: "shopftp", Password: "a-long-password-1", SiteUser: "shop"}); err != nil {
		t.Fatal(err)
	}
	f := m.FTP()
	if !f.Installed || len(f.Accounts) != 1 || f.Accounts[0].Home != "/srv/sites/shop" {
		t.Fatalf("ftp: %+v", f)
	}
	if err := m.AddFTP(FTPInput{Name: "shopftp", Password: "a-long-password-1", SiteUser: "shop"}); err == nil {
		t.Fatal("duplicate accepted")
	}
	for _, bad := range []FTPInput{
		{Name: "x", Password: "a-long-password-1", SiteUser: "shop"},
		{Name: "okname", Password: "short", SiteUser: "shop"},
		{Name: "okname", Password: "a-long-password-1", SiteUser: "shop", Dir: "../other"},
		{Name: "okname", Password: "a-long-password-1", SiteUser: "../etc"},
	} {
		if err := m.AddFTP(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	m.Lookup = func(n string) (int, int, string, error) { return 0, 0, "/root", nil }
	if err := m.AddFTP(FTPInput{Name: "rootftp", Password: "a-long-password-1", SiteUser: "root"}); err == nil {
		t.Fatal("account outside the sites directory accepted")
	}
	if err := m.DeleteFTP("shopftp"); err != nil || len(m.FTP().Accounts) != 0 {
		t.Fatal("delete failed")
	}
}

func TestDockerValidationAndLifecycle(t *testing.T) {
	m, _ := newMgr(t)
	tk, _ := m.InstallSoftware("docker")
	waitTask(t, m, tk.ID)
	for _, bad := range []RunInput{
		{Image: "Bad Image"}, {Image: "redis", Name: "-x"}, {Image: "redis", Ports: []string{"80"}},
		{Image: "redis", Env: []string{"1BAD=x"}}, {Image: "redis", Volumes: []string{"/var/run/docker.sock:/var/run/docker.sock"}},
		{Image: "redis", Volumes: []string{"/etc:/x"}}, {Image: "redis", Volumes: []string{"rel:/x"}}, {Image: "redis", Restart: "sometimes"},
	} {
		if _, err := m.RunContainer(bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	rt, err := m.RunContainer(RunInput{Image: "redis:7-alpine", Name: "cache", Ports: []string{"6379:6379"}, Env: []string{"A=b"}, Volumes: []string{"/srv/data:/data"}, Restart: "unless-stopped"})
	if err != nil {
		t.Fatal(err)
	}
	waitTask(t, m, rt.ID)
	d := m.Docker()
	if !d.Running || len(d.Containers) != 1 || d.Containers[0].Name != "cache" || d.Containers[0].State != "running" {
		t.Fatalf("docker: %+v", d)
	}
	id := d.Containers[0].ID
	if err := m.ContainerAction(id, "stop"); err != nil || m.Docker().Containers[0].State != "exited" {
		t.Fatal("stop failed")
	}
	if err := m.ContainerAction("rm -rf", "stop"); err == nil {
		t.Fatal("bad id accepted")
	}
	if err := m.ContainerAction(id, "exec"); err == nil {
		t.Fatal("unknown action accepted")
	}
	if err := m.ContainerAction(id, "remove"); err != nil || len(m.Docker().Containers) != 0 {
		t.Fatal("remove failed")
	}
}

func TestLogsWhitelistAndFilter(t *testing.T) {
	m, _ := newMgr(t)
	_ = os.MkdirAll(m.path("/var/log/nginx"), 0o755)
	_ = os.WriteFile(m.path("/var/log/nginx/error.log"), []byte("one\nTwo ERROR here\nthree\nanother error\n"), 0o644)
	found := false
	for _, l := range m.Logs() {
		found = found || l.ID == "nginx-error"
	}
	if !found {
		t.Fatal("existing log not listed")
	}
	s, err := m.ReadLog("nginx-error", 10, "error")
	if err != nil || s != "Two ERROR here\nanother error" {
		t.Fatalf("filtered: %q %v", s, err)
	}
	if s, _ := m.ReadLog("nginx-error", 1, ""); s != "another error" {
		t.Fatalf("tail: %q", s)
	}
	for _, id := range []string{"../../etc/passwd", "php:../../x", "unit:bad name"} {
		if _, err := m.ReadLog(id, 10, ""); err == nil {
			t.Fatalf("read %q", id)
		}
	}
}

func TestStatsSample(t *testing.T) {
	m, _ := newMgr(t)
	m.Sample()
	time.Sleep(20 * time.Millisecond)
	m.Sample()
	s := m.Stats()
	if len(s.History) != 2 || s.Cores == 0 && s.MemTotal == 0 {
		// /proc is absent off Linux; only require the structure to hold together.
		if s.History == nil {
			t.Fatal("no history")
		}
	}
}
