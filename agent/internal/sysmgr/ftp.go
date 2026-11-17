package sysmgr

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

type FTPAccount struct {
	Name string `json:"name"`
	UID  int    `json:"uid"`
	Home string `json:"home"`
	User string `json:"user"` // the Site User the account acts as
}

type FTP struct {
	Installed  bool         `json:"installed"`
	Running    bool         `json:"running"`
	Accounts   []FTPAccount `json:"accounts"`
	PassiveMin int          `json:"passive_min"`
	PassiveMax int          `json:"passive_max"`
}

const (
	ftpPasswd  = "/etc/pure-ftpd/pureftpd.passwd"
	ftpConfDir = "/etc/pure-ftpd/conf"
)

// ftpConfigure points Pure-FTPd at a PureDB virtual-user database and locks every account into its home.
func (m *Manager) ftpConfigure() error {
	conf := map[string]string{
		"ChrootEveryone":   "yes\n",
		"NoAnonymous":      "yes\n",
		"CreateHomeDir":    "no\n",
		"PureDB":           "/etc/pure-ftpd/pureftpd.pdb\n",
		"PassivePortRange": "40000 40100\n",
		"MinUID":           "100\n",
		"Umask":            "133:022\n",
		"DontResolve":      "yes\n",
	}
	for k, v := range conf {
		if err := m.write(ftpConfDir+"/"+k, v, 0o644); err != nil {
			return err
		}
	}
	// pure-ftpd tries authentication backends in the order of the files in auth/; this one enables PureDB.
	_ = m.Run.Run("ln", "-sf", ftpConfDir+"/PureDB", "/etc/pure-ftpd/auth/50pure")
	return nil
}

func (m *Manager) FTP() FTP {
	f := FTP{Accounts: []FTPAccount{}, PassiveMin: 40000, PassiveMax: 40100}
	if len(m.dpkgState([]string{"pure-ftpd"})) == 0 {
		return f
	}
	f.Installed = true
	f.Running = m.unitStates([]string{"pure-ftpd.service"})["pure-ftpd.service"].Active
	for _, l := range strings.Split(m.read(ftpPasswd), "\n") {
		p := strings.Split(l, ":")
		if len(p) < 6 || p[0] == "" {
			continue
		}
		f.Accounts = append(f.Accounts, FTPAccount{Name: p[0], UID: atoi(p[2]), Home: strings.TrimSuffix(strings.TrimSuffix(p[5], "/./"), "/"), User: m.userOfUID(atoi(p[2]))})
	}
	return f
}

func (m *Manager) userOfUID(uid int) string {
	return uidName(fmt.Sprint(uid))
}

var ftpNameRe = regexp.MustCompile(`^[a-z][a-z0-9_.-]{2,31}$`)

type FTPInput struct {
	Name     string `json:"name"`
	Password string `json:"password"`
	SiteUser string `json:"site_user"`
	Dir      string `json:"dir"` // folder below the site home, "" = the whole home
}

func (m *Manager) ftpHome(siteUser, dir string) (uid, gid int, home string, err error) {
	if !nameRe.MatchString(siteUser) {
		return 0, 0, "", invalid("site user")
	}
	uid, gid, h, err := m.Lookup(siteUser)
	if err != nil {
		return 0, 0, "", invalid("site user %q does not exist", siteUser)
	}
	base := filepath.Clean(m.SitesBase)
	if rel, rerr := filepath.Rel(base, filepath.Clean(h)); rerr != nil || strings.HasPrefix(rel, "..") {
		return 0, 0, "", invalid("%q is not a site user", siteUser)
	}
	full := filepath.Clean(filepath.Join(h, dir))
	if rel, rerr := filepath.Rel(filepath.Clean(h), full); rerr != nil || strings.HasPrefix(rel, "..") {
		return 0, 0, "", invalid("folder must stay inside the site")
	}
	return uid, gid, full, nil
}

func (m *Manager) AddFTP(in FTPInput) error {
	if !ftpNameRe.MatchString(in.Name) {
		return invalid("username: 3–32 characters, lowercase letters, digits, dot, dash, underscore")
	}
	if len(in.Password) < 10 || len(in.Password) > 128 || strings.ContainsAny(in.Password, "\r\n") {
		return invalid("password must be 10–128 characters")
	}
	for _, a := range m.FTP().Accounts {
		if a.Name == in.Name {
			return fmt.Errorf("%w: account exists", ErrConflict)
		}
	}
	uid, gid, home, err := m.ftpHome(in.SiteUser, in.Dir)
	if err != nil {
		return err
	}
	// pure-pw asks for the password twice on stdin.
	return m.Run.RunIn(in.Password+"\n"+in.Password+"\n", "pure-pw", "useradd", in.Name, "-u", fmt.Sprint(uid), "-g", fmt.Sprint(gid), "-d", home, "-m")
}

func (m *Manager) SetFTPPassword(name, password string) error {
	if !ftpNameRe.MatchString(name) {
		return invalid("username")
	}
	if len(password) < 10 || len(password) > 128 || strings.ContainsAny(password, "\r\n") {
		return invalid("password must be 10–128 characters")
	}
	return m.Run.RunIn(password+"\n"+password+"\n", "pure-pw", "passwd", name, "-m")
}

func (m *Manager) DeleteFTP(name string) error {
	if !ftpNameRe.MatchString(name) {
		return invalid("username")
	}
	return m.Run.Run("pure-pw", "userdel", name, "-m")
}

// OpenFTPPorts adds the firewall rules FTP needs (control port and the passive range).
func (m *Manager) OpenFTPPorts() error {
	for _, r := range []RuleInput{
		{Action: "allow", Port: "21", Proto: "tcp", Comment: "FTP"},
		{Action: "allow", Port: "40000:40100", Proto: "tcp", Comment: "FTP passive"},
	} {
		if err := m.AddRule(r); err != nil {
			return err
		}
	}
	return nil
}
