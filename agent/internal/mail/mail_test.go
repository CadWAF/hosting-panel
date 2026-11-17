package mail

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"hostingpanel/agent/internal/system"
)

func newMgr(t *testing.T) (*Manager, *system.DryRunner, string) {
	dir := t.TempDir()
	r := &system.DryRunner{}
	m, err := New(Config{StateDir: filepath.Join(dir, "state"), MailRoot: filepath.Join(dir, "vhosts"), Hostname: "mail.example.com",
		ServerIPv4: "203.0.113.10", WebmailURL: "https://webmail.example.com"}, r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m, r, dir
}

func TestDomainCreatesDKIMKeyAndChecklist(t *testing.T) {
	m, _, dir := newMgr(t)
	d, err := m.AddDomain("Example.org")
	if err != nil || d.Name != "example.org" {
		t.Fatal(d, err)
	}
	key, _ := os.ReadFile(filepath.Join(dir, "state", "dkim", "example.org.mail.key"))
	if !strings.Contains(string(key), "RSA PRIVATE KEY") {
		t.Fatal("dkim private key must be stored")
	}
	recs, err := m.Checklist("example.org")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Record{}
	for _, r := range recs {
		by[r.Type+" "+r.Name] = r
	}
	for _, k := range []string{"MX example.org", "A mail.example.com", "TXT example.org", "TXT mail._domainkey.example.org", "TXT _dmarc.example.org", "PTR 203.0.113.10"} {
		if _, ok := by[k]; !ok {
			t.Errorf("missing record %q in %v", k, by)
		}
	}
	if !strings.HasPrefix(by["TXT mail._domainkey.example.org"].Value, "v=DKIM1; k=rsa; p=MII") {
		t.Errorf("dkim value: %s", by["TXT mail._domainkey.example.org"].Value)
	}
	if by["PTR 203.0.113.10"].Where != "host provider" {
		t.Error("PTR is set at the hosting provider, say so")
	}
	if _, err := m.AddDomain("example.org"); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
	if _, err := m.AddDomain("not a domain"); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

type fakeDNS struct{ txt map[string][]string }

func (f fakeDNS) LookupMX(_ context.Context, n string) ([]*net.MX, error) {
	if n == "example.org" {
		return []*net.MX{{Host: "mail.example.com.", Pref: 10}}, nil
	}
	return nil, errors.New("nx")
}
func (f fakeDNS) LookupTXT(_ context.Context, n string) ([]string, error) { return f.txt[n], nil }
func (f fakeDNS) LookupHost(_ context.Context, n string) ([]string, error) {
	if n == "mail.example.com" {
		return []string{"198.51.100.9"}, nil // wrong IP on purpose
	}
	return nil, nil
}
func (f fakeDNS) LookupAddr(context.Context, string) ([]string, error) { return nil, nil }

func TestVerifyReportsOkWrongMissing(t *testing.T) {
	m, _, _ := newMgr(t)
	_, _ = m.AddDomain("example.org")
	recs, _ := m.Checklist("example.org")
	var dkim string
	for _, r := range recs {
		if strings.HasPrefix(r.Value, "v=DKIM1") {
			dkim = r.Value
		}
	}
	res, err := m.Verify(context.Background(), "example.org", fakeDNS{txt: map[string][]string{
		"example.org":                 {"v=spf1 mx -all"},
		"mail._domainkey.example.org": {dkim[:60] + `" "` + dkim[60:]}, // chunked, as DNS servers return long TXT
	}})
	if err != nil {
		t.Fatal(err)
	}
	st := map[string]string{}
	for _, r := range res {
		st[r.Type+" "+r.Name] = r.Status
	}
	want := map[string]string{
		"MX example.org":                  "ok",
		"A mail.example.com":              "wrong",
		"TXT example.org":                 "ok",
		"TXT mail._domainkey.example.org": "ok",
		"TXT _dmarc.example.org":          "missing",
		"PTR 203.0.113.10":                "missing",
	}
	for k, v := range want {
		if st[k] != v {
			t.Errorf("%s: got %s want %s", k, st[k], v)
		}
	}
}

func TestMailboxPasswordHashedAndExportedForPostfixDovecot(t *testing.T) {
	m, r, dir := newMgr(t)
	if _, err := m.AddMailbox("a@example.org", "long enough pw"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("domain must exist first: %v", err)
	}
	_, _ = m.AddDomain("example.org")
	if _, err := m.AddMailbox("a@example.org", "short"); !errors.Is(err, ErrInvalid) {
		t.Fatal("short password accepted")
	}
	for _, bad := range []string{"a b@example.org", "@example.org", "a@@example.org", "a';drop@example.org"} {
		if _, err := m.AddMailbox(bad, "long enough pw"); !errors.Is(err, ErrInvalid) {
			t.Errorf("%q accepted", bad)
		}
	}
	mb, err := m.AddMailbox("Info@Example.org", "long enough pw")
	if err != nil || mb.Email != "info@example.org" {
		t.Fatal(mb, err)
	}
	if !strings.Contains(strings.Join(r.Calls, "\n"), "vmail") {
		t.Error("maildir must be created owned by vmail")
	}
	out, err := sql.Open("sqlite", filepath.Join(dir, "state", "mailmap.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	var pw, maildir string
	if err := out.QueryRow(`SELECT password,maildir FROM mailboxes WHERE email='info@example.org'`).Scan(&pw, &maildir); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pw, "{BLF-CRYPT}$2") || maildir != "example.org/info/" {
		t.Fatalf("%q %q", pw, maildir)
	}
	if bcrypt.CompareHashAndPassword([]byte(strings.TrimPrefix(pw, "{BLF-CRYPT}")), []byte("long enough pw")) != nil {
		t.Fatal("stored hash does not verify")
	}
	var n int
	_ = out.QueryRow(`SELECT COUNT(*) FROM domains WHERE name='example.org'`).Scan(&n)
	if n != 1 {
		t.Fatal("domain must be exported")
	}
	if err := m.DeleteDomain("example.org"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("domain with mailboxes must not be deleted: %v", err)
	}
	if err := m.SetPassword(mb.ID, "another long pw"); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteMailbox(mb.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteDomain("example.org"); err != nil {
		t.Fatal(err)
	}
}

func TestWebmailLoginTokenIsSingleUseAndExpires(t *testing.T) {
	m, _, _ := newMgr(t)
	_, _ = m.AddDomain("example.org")
	mb, _ := m.AddMailbox("me@example.org", "long enough pw")
	url, err := m.IssueSSO(mb.ID)
	if err != nil || !strings.HasPrefix(url, "https://webmail.example.com/?_task=login&_hp_sso=") {
		t.Fatal(url, err)
	}
	tok := url[strings.LastIndex(url, "=")+1:]
	email, err := m.ConsumeSSO(tok)
	if err != nil || email != "me@example.org" {
		t.Fatal(email, err)
	}
	if _, err := m.ConsumeSSO(tok); !errors.Is(err, ErrNotFound) {
		t.Fatal("token must be single use")
	}
	url, _ = m.IssueSSO(mb.ID)
	tok = url[strings.LastIndex(url, "=")+1:]
	m.now = func() time.Time { return time.Now().Add(SSOTTL + time.Second) }
	if _, err := m.ConsumeSSO(tok); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired token accepted")
	}
	if _, err := m.ConsumeSSO("garbage"); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown token accepted")
	}
}

func TestStackFilesPointAtTheExportAndNeverLeakTheMasterPasswordHashPlain(t *testing.T) {
	m, _, dir := newMgr(t)
	files, err := m.StackFiles("MASTERSECRET", "/run/php/php8.3-fpm.sock")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]File{}
	for _, f := range files {
		by[f.Path] = f
	}
	mapdb := filepath.Join(dir, "state", "mailmap.db")
	for _, p := range []string{"/etc/postfix/hp-domains.cf", "/etc/postfix/hp-mailboxes.cf", "/etc/postfix/hp-aliases.cf", "/etc/dovecot/hp-sql.conf.ext"} {
		if !strings.Contains(by[p].Content, mapdb) {
			t.Errorf("%s must read the export db", p)
		}
	}
	if strings.Contains(by["/etc/dovecot/hp-master-users"].Content, "MASTERSECRET") {
		t.Error("dovecot master file must hold only a hash")
	}
	if !strings.Contains(by["/etc/roundcube/hp_sso.inc.php"].Content, "MASTERSECRET") || by["/etc/roundcube/hp_sso.inc.php"].Mode != 0o640 {
		t.Error("plugin config holds the master secret and must not be world readable")
	}
	for _, p := range []string{"/etc/dovecot/hp-master-users", "/etc/dovecot/hp-sql.conf.ext", "/etc/postfix/hp-mailboxes.cf"} {
		if by[p].Mode&0o007 != 0 {
			t.Errorf("%s is world readable", p)
		}
	}
	if !strings.Contains(by["/var/lib/roundcube/plugins/hp_sso/hp_sso.php"].Content, "storage_connect") {
		t.Error("plugin must hook storage_connect")
	}
	if !strings.Contains(by["/etc/nginx/sites-available/hp-webmail.conf"].Content, "server_name webmail.example.com;") {
		t.Error("webmail vhost host")
	}
	for _, s := range m.PostfixSettings() {
		if strings.Contains(s, "mynetworks") && strings.Contains(s, "0.0.0.0") {
			t.Error("open relay")
		}
	}
	if !strings.Contains(strings.Join(m.PostfixSettings(), "\n"), "smtpd_recipient_restrictions=permit_mynetworks,permit_sasl_authenticated,reject_unauth_destination") {
		t.Error("must not be an open relay")
	}
}

func TestEnableRoundcubePluginAppendsPHPOnceWithoutShellMangling(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.inc.php")
	os.WriteFile(p, []byte("<?php\n$config['plugins'] = [];\n"), 0o644)
	for i := 0; i < 2; i++ {
		if err := enableRoundcubePlugin(p); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(p)
	if strings.Count(string(b), "hp_sso") != 1 || !strings.Contains(string(b), "$config['plugins'] = array_merge($config['plugins'] ?? [], ['hp_sso']);") {
		t.Fatalf("bad config:\n%s", b)
	}
}
