# Hosting Panel

A self-hosted control panel for **one server and one administrator**. It manages web sites, PHP runtimes, databases, mail, backups, Docker, system software and the firewall from a single web UI, in English and Russian.

The Panel itself has no privileges over the system. Everything that needs root is done by small independent Services that the Panel talks to over Unix sockets, so a failure in one of them never takes the others down.

> Status: used on a real Ubuntu 24.04 host, but young. See [Known limits](#known-limits) before you rely on it.

## Contents

- [Features](#features)
- [Architecture](#architecture)
- [Requirements](#requirements)
- [Installation](#installation)
- [First login](#first-login)
- [Updating](#updating)
- [CadWAF Firewall](#cadwaf-firewall)
- [Development](#development)
- [Repository layout](#repository-layout)
- [Security model](#security-model)
- [Known limits](#known-limits)

## Features

**Sites**
- Create, disable, enable and delete sites; each runs as its own Linux user with its own PHP-FPM pool.
- nginx (default) or Apache behind nginx; several PHP versions side by side; per-site `php.ini` settings and presets.
- HTTPS through Let's Encrypt (certbot, HTTP-01).
- Web settings per site: reverse proxy to applications or Docker containers (with WebSocket support), redirects, upload limit, gzip, static caching, basic auth.
- Composer on sites where you allow it; phpMyAdmin installable as a site app (checksum verified).
- SSH keys (paste, upload or generate), cron jobs, logs.
- File manager: multi-select, copy and move, zip, search, previews, drag-and-drop upload.

**Databases**
- Install MariaDB, MySQL or PostgreSQL (one version per engine); create databases with a same-named user, reset passwords.
- Server statistics: version, uptime, data size, open connections, query count, database list.
- Removing a database server is refused while it holds any database, whether the Panel created it or not. If the server cannot be asked, nothing is removed.

**Mail**
- Postfix, Dovecot, Rspamd and Roundcube, a DNS checklist and one-click webmail login.

**Backups**
- Local, SFTP and Borg targets, schedules, retention and restore.

**System**
- Live monitoring (CPU, memory, load, network, disk, top processes), software installer, services, system updates, Docker, FTP, fail2ban, UFW.
- [CadWAF Firewall](#cadwaf-firewall): an eBPF/XDP default-deny firewall as an alternative to UFW.

**Notifications**
- Telegram and email alerts from a Notifier that runs independently of the Panel.

**Panel**
- Password and TOTP login, English and Russian interface, light and dark theme.

## Architecture

```
Browser ──HTTPS──▶ nginx ──▶ Panel (Laravel, own php-fpm, no privileges)
                                   │  JSON over Unix sockets in /run/hostingpanel/
        ┌───────────┬──────────────┼──────────────┬────────────┬───────────┐
   core-agent    dbmanager       backup      mail-service   system-service  notifier
   (sites, PHP,  (MariaDB,      (borg, sftp, (Postfix,      (monitoring,    (Telegram,
    nginx)        MySQL, PG)     schedules)   Dovecot...)    software,       email)
                                                             firewall,
                                                             Docker, FTP)
```

- `panel/` is Laravel 13 with Inertia and Vue 3, Tailwind and shadcn-vue components. It runs in its own php-fpm master (`hostingpanel-panel-fpm`), separate from site runtimes, so reloading a site's PHP never interrupts the Panel.
- `agent/` holds the Go Services. Each is a systemd unit speaking JSON over HTTP on a Unix socket. The Panel only calls an explicit whitelist of methods (`RpcController`).
- Long operations (installs, updates, restores) run as tasks whose log the UI follows live.
- Design notes: [GLOSSARY.md](GLOSSARY.md) and [docs/adr](docs/adr).

## Requirements

- A fresh or dedicated server running **Ubuntu 24.04 to 26.04**, root access, a public IP.
- A domain name for the Panel pointing at the server (needed for HTTPS).
- To build: Go (see `agent/go.mod`), Node.js with npm, PHP 8.4 with Composer or Docker, `rsync`, `make`.

## Installation

Build the distribution on your computer, copy it to the server, run the installer:

```bash
make dist
rsync -az dist/ root@server:/root/dist/
ssh root@server 'cd /root/dist && ./install.sh panel.example.com'
```

`make dist` cross-compiles the Services for Linux (amd64) and builds the front-end. `install.sh`:

1. installs nginx, certbot, PHP 8.4 (from the ondrej PPA), Composer, sqlite3 and the other dependencies;
2. places the Services in `/usr/local/bin` and the Panel in `/opt/hostingpanel/panel`;
3. creates the systemd units (`hostingpanel-core`, `-dbmanager`, `-backup`, `-mail`, `-system`, `-notifier`, `-panel-fpm`) and the nginx vhost for the Panel;
4. runs the migrations and starts everything.

Re-running `install.sh` on an installed server is an update: `.env`, storage and the database are kept and the HTTPS certificate is re-applied. After an update restart the Services whose binary changed (for example `systemctl restart hostingpanel-system`).

### Turn on HTTPS

```bash
certbot --nginx -d panel.example.com
```

If the Panel is behind Cloudflare, the vhost already trusts `CF-Connecting-IP` from Cloudflare's ranges, so the Panel sees visitors' real addresses.

### Notifications

Edit `/etc/hostingpanel/notifier.json` (see `deploy/notifier.example.json`) for Telegram and SMTP, then:

```bash
systemctl restart hostingpanel-notifier
```

## First login

Create the single administrator on the server. The command prints the TOTP secret to add to your authenticator app:

```bash
cd /opt/hostingpanel/panel && sudo -u hostingpanel php artisan panel:admin you@example.com
```

Open the Panel's domain and sign in with the password and a TOTP code.

## Updating

Run the three installation commands again with the new build. Nothing is reset. The Panel never updates itself.

## CadWAF Firewall

[CadWAF Firewall](https://github.com/CadWAF/firewall) is an open-source eBPF/XDP host firewall: default-deny, per-port rules, allow and block lists and a record of dropped traffic. The Panel manages it from **Security → CadWAF**.

- **Install** downloads the newest published release for the server's CPU, verifies its SHA-256 against the digest GitHub publishes and refuses to install it without one. If GitHub is unreachable it falls back to an optional offline copy: run `make dist FIREWALL_SRC=/path/to/firewall-checkout` to bundle one.
- At first start the ports already in use (SSH, 80, 443 and anything listening publicly) stay open and the administrator's current address is allow-listed. Outbound filtering starts off.
- **Update** appears when a newer release is published. If the new version does not come up, the previous binaries are put back.
- Manage ports, allow and block lists, rules and outbound filtering, and read the drop log. **Emergency off** detaches the filter.
- Keep a second SSH session open the first time. Turn UFW off once you have checked that CadWAF works; both together would filter twice.

The eBPF program must pass the kernel verifier on your kernel (5.10 or newer). If the service refuses to start, `journalctl -u firewalld` shows why.

## Development

No root needed. The dev stack runs the Panel in Docker with all Services in dry-run mode (host commands are logged, not executed):

```bash
docker compose -f dev/docker-compose.yml up -d      # Panel on http://localhost:8200
python3 dev/totp.py                                   # current TOTP code for the dev login
```

Dev login: `admin@local.test` / `dev-password-1234`. These are development-only values.

After changing Go code, rebuild the binary and restart the stack:

```bash
cd agent && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../dev/bin/system-service ./cmd/system
docker compose -f dev/docker-compose.yml restart
```

Tests:

```bash
cd agent && go test ./...
docker compose -f dev/docker-compose.yml exec -T panel sh -c 'cd /app && php artisan test'
cd panel && npm run build
```

Interface text is English; the English string is the translation key (`t('...')`). Russian lives in `panel/resources/js/locales/ru.ts`.

## Repository layout

| Path | Contents |
|------|----------|
| `agent/` | Go Services: `cmd/` entry points, `internal/` packages (sites, dbmanager, sysmgr, backup, mail, notifier, store, tasks) |
| `panel/` | Laravel 13 Panel: controllers and service clients in `app/`, Inertia pages and components in `resources/js/` |
| `deploy/` | `install.sh`, systemd units, nginx and php-fpm configs |
| `dev/` | Docker development stack |
| `docs/adr/` | Architecture decision records |
| `GLOSSARY.md` | Domain vocabulary used in code and UI |

## Security model

- One administrator; password plus TOTP. There is no registration and no second role.
- The Panel runs unprivileged. Privileged work happens in Services reachable only through Unix sockets, behind an explicit method whitelist.
- Sites run as separate Linux users with their own PHP-FPM pools; the file manager is confined to the site's directory.
- Downloads of third-party software (phpMyAdmin, CadWAF) are checksum verified.
- Secrets such as generated database and site passwords are shown once and not stored.

Report vulnerabilities privately to the repository owner rather than in a public issue.

## Known limits

- Outbound mail delivery and DKIM signing are not verified end to end (they need port 25 and DNS).
- Dovecot 2.4 is not supported.
- Per-site resource limits are presets, not cgroups.
- The file manager is jailed with `os.Root` inside the root-run agent rather than running as the site user.
- Services speak JSON over HTTP on Unix sockets, not gRPC.
- Composer as the site user, nginx basic auth and WebSocket proxying, and the file download and zip routes were exercised in the development stack and tests but have had little time on production hosts.
- Linux amd64 servers are built by `make dist`; arm64 builds of the Services are not produced.

## License

Free for **personal, non-commercial use only**. Commercial use, including running it for clients, an employer or a business, selling it or offering it as a service, needs the author's written permission. See [LICENSE](LICENSE).
