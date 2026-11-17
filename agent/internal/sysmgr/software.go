package sysmgr

import (
	"fmt"
	"strings"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/tasks"
)

// Package is one entry of the software catalog: an apt-installable thing the Administrator can add in one click.
type Package struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Category    string   `json:"category"`
	Description string   `json:"description"`
	Packages    []string `json:"packages"`
	Units       []string `json:"units"`
	// Manage is the Panel page that configures it once installed ("" = nothing more to configure).
	Manage string `json:"manage"`
	// Detail is a short note shown before installing (side effects, what it opens).
	Detail string `json:"detail"`
}

// Software is a catalog entry with live state.
type Software struct {
	Package
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Active    bool   `json:"active"`
	Enabled   bool   `json:"enabled"`
}

func catalog() []Package {
	return []Package{
		// Security
		{ID: "fail2ban", Name: "Fail2ban", Category: "security", Description: "Bans IP addresses that keep failing to log in over SSH, nginx and mail.", Packages: []string{"fail2ban"}, Units: []string{"fail2ban.service"}, Manage: "/security", Detail: "Enables the sshd jail with safe defaults."},
		{ID: "ufw", Name: "UFW firewall", Category: "security", Description: "Simple host firewall. Manage rules in Security.", Packages: []string{"ufw"}, Units: []string{"ufw.service"}, Manage: "/security", Detail: "Installed but left disabled until you turn it on."},
		{ID: "unattended-upgrades", Name: "Automatic security updates", Category: "security", Description: "Installs security updates in the background.", Packages: []string{"unattended-upgrades"}, Units: []string{"unattended-upgrades.service"}, Manage: "/updates"},
		{ID: "clamav", Name: "ClamAV antivirus", Category: "security", Description: "Scans files for malware (on demand and with the signature updater).", Packages: []string{"clamav", "clamav-daemon", "clamav-freshclam"}, Units: []string{"clamav-freshclam.service", "clamav-daemon.service"}, Detail: "Uses about 1 GB of RAM while the daemon runs."},
		// Runtimes and tools
		{ID: "docker", Name: "Docker", Category: "containers", Description: "Run applications in containers.", Packages: []string{"docker.io", "docker-compose-v2"}, Units: []string{"docker.service"}, Manage: "/docker"},
		{ID: "nodejs", Name: "Node.js", Category: "runtime", Description: "JavaScript runtime and npm.", Packages: []string{"nodejs", "npm"}},
		{ID: "python", Name: "Python 3 + pip", Category: "runtime", Description: "Python interpreter, pip and virtualenv.", Packages: []string{"python3", "python3-pip", "python3-venv"}},
		{ID: "java", Name: "Java 21 (headless)", Category: "runtime", Description: "OpenJDK runtime, used by Minecraft and many game servers.", Packages: []string{"openjdk-21-jre-headless"}},
		{ID: "git", Name: "Git", Category: "runtime", Description: "Version control.", Packages: []string{"git"}},
		{ID: "composer", Name: "Composer", Category: "runtime", Description: "PHP dependency manager.", Packages: []string{"composer"}},
		{ID: "supervisor", Name: "Supervisor", Category: "runtime", Description: "Keeps long-running processes (queues, bots, game servers) alive.", Packages: []string{"supervisor"}, Units: []string{"supervisor.service"}},
		// Cache and queues
		{ID: "redis", Name: "Redis", Category: "data", Description: "In-memory cache, sessions and queues.", Packages: []string{"redis-server"}, Units: []string{"redis-server.service"}, Detail: "Listens on 127.0.0.1 only."},
		{ID: "memcached", Name: "Memcached", Category: "data", Description: "Fast in-memory object cache.", Packages: []string{"memcached"}, Units: []string{"memcached.service"}, Detail: "Listens on 127.0.0.1 only."},
		{ID: "rabbitmq", Name: "RabbitMQ", Category: "data", Description: "Message broker.", Packages: []string{"rabbitmq-server"}, Units: []string{"rabbitmq-server.service"}},
		// File transfer
		{ID: "pure-ftpd", Name: "Pure-FTPd", Category: "transfer", Description: "FTP server with virtual accounts tied to sites. Manage accounts in FTP.", Packages: []string{"pure-ftpd"}, Units: []string{"pure-ftpd.service"}, Manage: "/ftp", Detail: "Opens port 21 and a passive range (40000–40100) once the firewall rules are added."},
		{ID: "rsync", Name: "rsync", Category: "transfer", Description: "Fast incremental file copy.", Packages: []string{"rsync"}},
		{ID: "rclone", Name: "rclone", Category: "transfer", Description: "Sync files with S3, Google Drive and many other clouds.", Packages: []string{"rclone"}},
		// Media
		{ID: "ffmpeg", Name: "FFmpeg", Category: "media", Description: "Convert audio and video.", Packages: []string{"ffmpeg"}},
		{ID: "imagemagick", Name: "ImageMagick", Category: "media", Description: "Command-line image processing.", Packages: []string{"imagemagick"}},
		// Monitoring and system
		{ID: "vnstat", Name: "vnStat", Category: "system", Description: "Long-term network traffic statistics.", Packages: []string{"vnstat"}, Units: []string{"vnstat.service"}},
		{ID: "chrony", Name: "Chrony", Category: "system", Description: "Keeps the server clock accurate.", Packages: []string{"chrony"}, Units: []string{"chrony.service"}},
		{ID: "htop", Name: "htop", Category: "system", Description: "Interactive process viewer for SSH sessions.", Packages: []string{"htop"}},
		{ID: "ncdu", Name: "ncdu", Category: "system", Description: "Find what is using disk space.", Packages: []string{"ncdu"}},
		{ID: "tmux", Name: "tmux", Category: "system", Description: "Terminal multiplexer for long SSH sessions.", Packages: []string{"tmux"}},
		{ID: "zip", Name: "zip / unzip", Category: "system", Description: "Archive tools.", Packages: []string{"zip", "unzip"}},
	}
}

func (m *Manager) pkg(id string) (Package, error) {
	for _, p := range m.catalog {
		if p.ID == id {
			return p, nil
		}
	}
	return Package{}, ErrNotFound
}

// dpkgState maps package name -> version for installed packages.
func (m *Manager) dpkgState(pkgs []string) map[string]string {
	out := m.out("dpkg-query", append([]string{"-W", "-f", "${Package}\t${db:Status-Abbrev}\t${Version}\n"}, pkgs...)...)
	res := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Split(l, "\t")
		if len(f) == 3 && strings.HasPrefix(strings.TrimSpace(f[1]), "ii") {
			res[f[0]] = f[2]
		}
	}
	return res
}

type unitState struct{ Active, Enabled bool }

// unitStates reports active/enabled for several units with two systemctl calls.
func (m *Manager) unitStates(units []string) map[string]unitState {
	res := map[string]unitState{}
	if len(units) == 0 {
		return res
	}
	act := strings.Split(strings.TrimSpace(m.out("systemctl", append([]string{"is-active"}, units...)...)), "\n")
	en := strings.Split(strings.TrimSpace(m.out("systemctl", append([]string{"is-enabled"}, units...)...)), "\n")
	for i, u := range units {
		s := unitState{}
		if i < len(act) {
			s.Active = strings.TrimSpace(act[i]) == "active"
		}
		if i < len(en) {
			s.Enabled = strings.TrimSpace(en[i]) == "enabled"
		}
		res[u] = s
	}
	return res
}

// Software lists the catalog with installed/running state.
func (m *Manager) Software() []Software {
	var pk, un []string
	for _, p := range m.catalog {
		pk = append(pk, p.Packages...)
		un = append(un, p.Units...)
	}
	ds := m.dpkgState(pk)
	us := m.unitStates(un)
	out := make([]Software, 0, len(m.catalog))
	for _, p := range m.catalog {
		s := Software{Package: p}
		if p.Packages == nil {
			p.Packages = []string{}
		}
		if v, ok := ds[p.Packages[0]]; ok {
			s.Installed, s.Version = true, v
		}
		for _, u := range p.Units {
			if st := us[u]; st.Active {
				s.Active = true
			}
			if st := us[u]; st.Enabled {
				s.Enabled = true
			}
		}
		if s.Units == nil {
			s.Units = []string{}
		}
		out = append(out, s)
	}
	return out
}

// InstallSoftware installs a catalog entry as a Task.
func (m *Manager) InstallSoftware(id string) (store.Task, error) {
	p, err := m.pkg(id)
	if err != nil {
		return store.Task{}, err
	}
	return m.Tasks.Start("software.install", "Install "+p.Name, func(l *tasks.Log) error {
		m.apt.Lock()
		defer m.apt.Unlock()
		if err := m.Run.RunW(l, "apt-get", "update", "-q"); err != nil {
			return err
		}
		if err := m.Run.RunW(l, "apt-get", append([]string{"-y", "-q", "-o", "Dpkg::Options::=--force-confold", "install"}, p.Packages...)...); err != nil {
			return err
		}
		if err := m.afterInstall(p.ID, l); err != nil {
			return fmt.Errorf("configure %s: %w", p.Name, err)
		}
		for _, u := range p.Units {
			l.Printf("Enabling %s", u)
			if err := m.Run.RunW(l, "systemctl", "enable", "--now", u); err != nil {
				l.Printf("warning: %v", err)
			}
		}
		l.Printf("%s is installed.", p.Name)
		return nil
	})
}

// RemoveSoftware removes a catalog entry's packages (configuration is kept).
func (m *Manager) RemoveSoftware(id string) (store.Task, error) {
	p, err := m.pkg(id)
	if err != nil {
		return store.Task{}, err
	}
	return m.Tasks.Start("software.remove", "Remove "+p.Name, func(l *tasks.Log) error {
		m.apt.Lock()
		defer m.apt.Unlock()
		if p.ID == "ufw" {
			_ = m.Run.RunW(l, "ufw", "disable")
		}
		for _, u := range p.Units {
			_ = m.Run.RunW(l, "systemctl", "disable", "--now", u)
		}
		if err := m.Run.RunW(l, "apt-get", append([]string{"-y", "-q", "remove"}, p.Packages...)...); err != nil {
			return err
		}
		return m.Run.RunW(l, "apt-get", "-y", "-q", "autoremove")
	})
}

// afterInstall applies the safe defaults a package needs to be useful from the Panel.
func (m *Manager) afterInstall(id string, l *tasks.Log) error {
	switch id {
	case "fail2ban":
		l.Printf("Writing fail2ban defaults")
		return m.fail2banDefaults()
	case "pure-ftpd":
		l.Printf("Configuring Pure-FTPd for virtual accounts")
		return m.ftpConfigure()
	case "unattended-upgrades":
		return m.write("/etc/apt/apt.conf.d/20auto-upgrades", "APT::Periodic::Update-Package-Lists \"1\";\nAPT::Periodic::Unattended-Upgrade \"1\";\n", 0o644)
	case "redis":
		return nil
	}
	return nil
}

// Categories in display order.
func Categories() []string {
	return []string{"security", "containers", "runtime", "data", "transfer", "media", "system"}
}
