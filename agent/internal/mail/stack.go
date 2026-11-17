package mail

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"hostingpanel/agent/internal/store"
	"hostingpanel/agent/internal/tasks"
)

// File is one config file the installer writes.
type File struct {
	Path    string
	Mode    os.FileMode
	Content string
}

const (
	masterUser = "hpsso"
	mailUID    = "5000" // vmail
)

func (m *Manager) mapDB() string { return filepath.Join(m.cfg.StateDir, "mailmap.db") }

// StackFiles returns every config file for Postfix, Dovecot, Rspamd, Roundcube and nginx.
// masterPass is the plaintext Dovecot master password (used only by the Roundcube plugin).
func (m *Manager) StackFiles(masterPass string, phpSocket string) ([]File, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(masterPass), 10)
	if err != nil {
		return nil, err
	}
	db := m.mapDB()
	files := []File{
		{"/etc/postfix/hp-domains.cf", 0o640, fmt.Sprintf("dbpath = %s\nquery = SELECT 1 FROM domains WHERE name='%%s'\n", db)},
		{"/etc/postfix/hp-mailboxes.cf", 0o640, fmt.Sprintf("dbpath = %s\nquery = SELECT maildir FROM mailboxes WHERE email='%%s'\n", db)},
		{"/etc/postfix/hp-aliases.cf", 0o640, fmt.Sprintf("dbpath = %s\nquery = SELECT destination FROM aliases WHERE source='%%s'\n", db)},

		{"/etc/dovecot/conf.d/99-hostingpanel.conf", 0o644, `# managed by hostingpanel mail service - do not edit
protocols = imap lmtp
mail_location = maildir:` + m.cfg.MailRoot + `/%d/%n
mail_uid = vmail
mail_gid = vmail
first_valid_uid = 5000
disable_plaintext_auth = yes
auth_mechanisms = plain login
auth_master_user_separator = *

passdb {
  driver = passwd-file
  args = /etc/dovecot/hp-master-users
  master = yes
  result_success = continue
}
passdb {
  driver = sql
  args = /etc/dovecot/hp-sql.conf.ext
}
userdb {
  driver = static
  args = uid=vmail gid=vmail home=` + m.cfg.MailRoot + `/%d/%n
}

service auth {
  unix_listener /var/spool/postfix/private/auth {
    mode = 0660
    user = postfix
    group = postfix
  }
}
service lmtp {
  unix_listener /var/spool/postfix/private/dovecot-lmtp {
    mode = 0600
    user = postfix
    group = postfix
  }
}
ssl = required
ssl_cert = <` + "/etc/ssl/certs/ssl-cert-snakeoil.pem" + `
ssl_key = <` + "/etc/ssl/private/ssl-cert-snakeoil.key" + `
`},
		{"/etc/dovecot/hp-sql.conf.ext", 0o640, fmt.Sprintf("driver = sqlite\nconnect = %s\ndefault_pass_scheme = BLF-CRYPT\npassword_query = SELECT email AS user, password FROM mailboxes WHERE email = '%%{user}'\n", db)},
		{"/etc/dovecot/hp-master-users", 0o640, masterUser + ":{BLF-CRYPT}" + string(h) + "\n"},

		{"/etc/rspamd/local.d/dkim_signing.conf", 0o644, fmt.Sprintf(`# managed by hostingpanel mail service
enabled = true;
path = "%s/dkim/$domain.$selector.key";
selector = "%s";
allow_username_mismatch = true;
use_esld = false;
sign_authenticated = true;
`, m.cfg.StateDir, m.cfg.Selector)},
		{"/etc/rspamd/local.d/worker-proxy.inc", 0o644, `milter = yes;
timeout = 120s;
upstream "local" {
  default = yes;
  self_scan = yes;
}
bind_socket = "localhost:11332";
`},

		{"/var/lib/roundcube/plugins/hp_sso/hp_sso.php", 0o644, roundcubePlugin},
		{"/etc/roundcube/hp_sso.inc.php", 0o640, fmt.Sprintf(`<?php
// managed by hostingpanel mail service
$hp_sso = [
  'socket' => '%s',
  'master_user' => '%s',
  'master_pass' => '%s',
];
`, SSOSocket, masterUser, masterPass)},

		{"/etc/nginx/sites-available/hp-webmail.conf", 0o644, m.webmailVhost(phpSocket)},
	}
	return files, nil
}

// SSOSocket is where the consume-only socket (readable by the web server) lives.
// It has its own directory: the shared /run/hostingpanel is closed to the web server on purpose.
const SSOSocket = "/run/hostingpanel-sso/mail-sso.sock"

func (m *Manager) webmailVhost(phpSocket string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(m.cfg.WebmailURL, "https://"), "http://")
	host = strings.TrimRight(host, "/")
	return fmt.Sprintf(`# managed by hostingpanel mail service
server {
    listen 80;
    listen [::]:80;
    server_name %s;
    root /usr/share/roundcube;
    index index.php;
    client_max_body_size 50m;
    location / { try_files $uri $uri/ /index.php?$args; }
    location ~ \.php$ {
        include snippets/fastcgi-php.conf;
        fastcgi_pass unix:%s;
    }
    location ~ ^/(config|temp|logs|SQL|bin|vendor)/ { deny all; }
}
`, host, phpSocket)
}

// PostfixSettings are applied with `postconf -e`.
func (m *Manager) PostfixSettings() []string {
	return []string{
		"myhostname=" + m.cfg.Hostname,
		"virtual_mailbox_domains=sqlite:/etc/postfix/hp-domains.cf",
		"virtual_mailbox_maps=sqlite:/etc/postfix/hp-mailboxes.cf",
		"virtual_alias_maps=sqlite:/etc/postfix/hp-aliases.cf",
		"virtual_mailbox_base=" + m.cfg.MailRoot,
		"virtual_uid_maps=static:" + mailUID,
		"virtual_gid_maps=static:" + mailUID,
		"virtual_transport=lmtp:unix:private/dovecot-lmtp",
		"smtpd_sasl_type=dovecot",
		"smtpd_sasl_path=private/auth",
		"smtpd_sasl_auth_enable=yes",
		"smtpd_tls_security_level=may",
		"smtp_tls_security_level=may",
		"smtpd_tls_auth_only=yes",
		"smtpd_recipient_restrictions=permit_mynetworks,permit_sasl_authenticated,reject_unauth_destination",
		"smtpd_milters=inet:localhost:11332",
		"non_smtpd_milters=inet:localhost:11332",
		"milter_protocol=6",
		"milter_default_action=accept",
		"mailbox_size_limit=0",
	}
}

const roundcubePlugin = `<?php
/**
 * Hosting Panel one-click login. A short-lived single-use token (issued by the panel) is
 * redeemed against the local mail service, then the session is opened through the Dovecot master user.
 */
class hp_sso extends rcube_plugin
{
    public $task = 'login|mail';
    private $cfg = [];

    public function init()
    {
        $hp_sso = [];
        @include '/etc/roundcube/hp_sso.inc.php';
        $this->cfg = $hp_sso;
        $this->add_hook('startup', [$this, 'startup']);
        $this->add_hook('storage_connect', [$this, 'storage_connect']);
    }

    public function startup($args)
    {
        $rc = rcmail::get_instance();
        $tok = rcube_utils::get_input_value('_hp_sso', rcube_utils::INPUT_GET);
        if ($tok && $args['task'] == 'login' && empty($_SESSION['user_id'])) {
            $email = $this->redeem($tok);
            if ($email) {
                $_SESSION['hp_sso'] = true;
                $host = $rc->autoselect_host();
                if ($rc->login($email, bin2hex(random_bytes(8)), $host, false)) {
                    $rc->session->remove('temp');
                    $rc->session->regenerate_id(false);
                    $rc->session->set_auth_cookie();
                    $rc->log_login();
                    $rc->output->redirect(['_task' => 'mail']);
                    exit;
                }
            }
            unset($_SESSION['hp_sso']);
        }
        return $args;
    }

    public function storage_connect($args)
    {
        if (!empty($_SESSION['hp_sso'])) {
            $args['user'] = $args['user'] . '*' . $this->cfg['master_user'];
            $args['pass'] = $this->cfg['master_pass'];
        }
        return $args;
    }

    private function redeem($token)
    {
        $ch = curl_init('http://mail/v1/sso/consume');
        curl_setopt_array($ch, [
            CURLOPT_UNIX_SOCKET_PATH => $this->cfg['socket'],
            CURLOPT_POST => true,
            CURLOPT_POSTFIELDS => json_encode(['token' => $token]),
            CURLOPT_HTTPHEADER => ['Content-Type: application/json'],
            CURLOPT_RETURNTRANSFER => true,
            CURLOPT_TIMEOUT => 5,
        ]);
        $body = curl_exec($ch);
        $code = curl_getinfo($ch, CURLINFO_RESPONSE_CODE);
        curl_close($ch);
        if ($code !== 200) {
            return null;
        }
        $j = json_decode($body, true);
        return $j['email'] ?? null;
    }
}
`

// ---- installer ----

func newSecret() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Installed reports whether the mail stack was set up (the master secret exists).
func (m *Manager) Installed() bool {
	_, err := os.Stat(filepath.Join(m.cfg.StateDir, "master.secret"))
	return err == nil
}

// InstallStack installs and configures the mail stack as a Task.
func (m *Manager) InstallStack(tm *tasks.Manager, phpSocket string) (store.Task, error) {
	if m.cfg.Hostname == "" || m.cfg.WebmailURL == "" {
		return store.Task{}, fmt.Errorf("%w: set the mail hostname and webmail URL first", ErrInvalid)
	}
	return tm.Start("mail.install", "Install mail server", func(l *tasks.Log) error {
		run := func(name string, args ...string) error { return m.run.RunW(l, name, args...) }
		for _, sel := range []string{
			"postfix postfix/main_mailer_type select Internet Site",
			"postfix postfix/mailname string " + m.cfg.Hostname,
			"roundcube-core roundcube/dbconfig-install boolean true",
			"roundcube-core roundcube/database-type select sqlite3",
		} {
			if err := m.run.RunIn(sel+"\n", "debconf-set-selections"); err != nil {
				return err
			}
		}
		l.Printf("Installing packages")
		if err := run("apt-get", "-y", "install", "postfix", "postfix-sqlite", "dovecot-core", "dovecot-imapd", "dovecot-lmtpd",
			"dovecot-sqlite", "rspamd", "redis-server", "php-fpm", "roundcube-core", "roundcube-sqlite3", "ssl-cert"); err != nil {
			return err
		}
		if out, err := exec.Command("dovecot", "--version").Output(); err == nil && !strings.HasPrefix(string(out), "2.3") {
			return fmt.Errorf("Dovecot %s is installed but only Dovecot 2.3 configuration is supported yet", strings.TrimSpace(string(out)))
		}
		_ = run("groupadd", "--system", "--gid", mailUID, "vmail")
		_ = run("useradd", "--system", "--uid", mailUID, "--gid", mailUID, "--home-dir", m.cfg.MailRoot, "--shell", "/usr/sbin/nologin", "vmail")
		if err := run("install", "-d", "-o", "vmail", "-g", "vmail", "-m", "0700", m.cfg.MailRoot); err != nil {
			return err
		}
		// postfix and dovecot read the exported map database through this group
		_ = run("usermod", "-aG", "vmail", "postfix")

		pass := newSecret()
		files, err := m.StackFiles(pass, phpSocket)
		if err != nil {
			return err
		}
		for _, f := range files {
			if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(f.Path, []byte(f.Content), f.Mode); err != nil {
				return err
			}
			l.Printf("wrote %s", f.Path)
		}
		_ = run("chgrp", "vmail", m.mapDB())
		_ = run("chgrp", "www-data", "/etc/roundcube/hp_sso.inc.php")
		_ = run("chgrp", "dovecot", "/etc/dovecot/hp-master-users", "/etc/dovecot/hp-sql.conf.ext")
		_ = run("ln", "-sf", "/etc/nginx/sites-available/hp-webmail.conf", "/etc/nginx/sites-enabled/hp-webmail.conf")
		for _, s := range m.PostfixSettings() {
			if err := run("postconf", "-e", s); err != nil {
				return err
			}
		}
		if err := run("postconf", "-M", "submission/inet=submission inet n - y - - smtpd"); err != nil {
			return err
		}
		for _, s := range []string{"syslog_name=postfix/submission", "smtpd_tls_security_level=encrypt", "smtpd_sasl_auth_enable=yes",
			"smtpd_recipient_restrictions=permit_sasl_authenticated,reject"} {
			if err := run("postconf", "-P", "submission/inet/"+s); err != nil {
				return err
			}
		}
		if err := enableRoundcubePlugin("/etc/roundcube/config.inc.php"); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(m.cfg.StateDir, "master.secret"), []byte(pass), 0o600); err != nil {
			return err
		}
		for _, svc := range []string{"redis-server", "rspamd", "dovecot", "postfix"} {
			if err := run("systemctl", "enable", "--now", svc); err != nil {
				return err
			}
			if err := run("systemctl", "restart", svc); err != nil {
				return err
			}
		}
		if err := run("nginx", "-t"); err != nil {
			return err
		}
		return run("systemctl", "reload", "nginx")
	})
}

// enableRoundcubePlugin appends the hp_sso plugin to Roundcube's config once.
func enableRoundcubePlugin(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if strings.Contains(string(b), "hp_sso") {
		return nil
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString("\n// hostingpanel\n$config['plugins'] = array_merge($config['plugins'] ?? [], ['hp_sso']);\n")
	return err
}
