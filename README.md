# Hosting Panel

Single-administrator web hosting panel for Ubuntu 24.04–26.04. Design decisions: [GLOSSARY.md](GLOSSARY.md), [docs/adr](docs/adr).

- `agent/` — Core Agent (Go): Sites, Site Users, PHP-FPM pools, nginx. JSON over a Unix socket.
- `panel/` — the Panel (Laravel 13, Livewire, Tailwind). No system privileges.
- `deploy/` — systemd units.

## Development (macOS/Linux, no root)

```bash
# Core Agent in dry-run: files go under a scratch dir, host commands are only logged
cd agent && go test ./... && go run ./cmd/core-agent -socket /tmp/core.sock -db /tmp/core.db -dry-run-root /tmp/hp-root

# Panel tests (PHP via Docker)
cd panel && docker run --rm -v "$PWD":/app -w /app composer:2 php artisan test

# Create the Administrator (prints the TOTP secret)
php artisan panel:admin you@example.com
```

## Install on a server

```bash
make dist                      # cross-compiles services, builds panel assets into dist/
scp -r dist root@server:/root/ && ssh root@server 'cd /root/dist && ./install.sh panel.example.com'
```

### CadWAF Firewall bundle (optional)

`make dist` also cross-compiles the eBPF/XDP firewall from `FIREWALL_SRC` (default empty) into `dist/firewall/<arch>/`; `install.sh` copies it to `/usr/share/hostingpanel/firewall/`. It is installed later from **Security → CadWAF** (it never starts filtering without the Administrator's confirmation, keeps SSH/80/443 and everything already listening open, and allow-lists the Administrator's address). Without the bundle the tab simply reports that the installer is missing.

## Status

Built: login (password + TOTP); Sites (create/disable/enable/delete, per-site user and PHP-FPM pool, nginx or Apache mode, php.ini, multiple PHP versions, presets); File Manager; SSH keys (paste/upload/generate), cron, logs; HTTPS via Let's Encrypt (certbot, HTTP-01); Databases (MariaDB/MySQL/PostgreSQL); Backups (local, SFTP, Borg, schedules, retention, restore); Mail (Postfix, Dovecot, Rspamd, Roundcube, DNS checklist, one-click webmail login); Notifier (Telegram/email, independent); per-site web settings (reverse proxy to Docker containers, redirects, upload limit, gzip, static cache, basic auth), opt-in Composer per site, phpMyAdmin as an installable site app, and a File Manager with multi-select, copy/move, zip, search, previews and drag-and-drop upload.

Panel runtime: the Panel runs in its own php-fpm master (`hostingpanel-panel-fpm`, PHP 8.4+), separate from site PHP runtimes, so reloading a site's PHP never interrupts the Panel. Verified on a real Ubuntu 24.04 host: sites, PHP-FPM pools, multiple PHP versions, Apache mode, SSH keys, cron, MariaDB, backups and restore, Postfix/Dovecot/Rspamd/Roundcube with one-click webmail login, HTTPS via certbot.

Known limits: outbound mail delivery and DKIM signing are not yet verified end to end (port 25 and DNS needed); Dovecot 2.4 is not supported; per-site resource limits are presets, not cgroups; the File Manager is jailed with `os.Root` inside the root-run agent rather than running as the Site User; services talk JSON over HTTP on Unix sockets (not gRPC). v2: firewall; later: WAF, Docker if needed.
