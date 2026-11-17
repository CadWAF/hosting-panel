# Hosting Panel

Single-administrator control panel for managing web sites, their runtimes, databases, mail, backups and notifications on one server (one Panel, one set of Services; no cluster).

## Language

**Administrator**:
The single human who logs into the Panel (password + TOTP) and manages everything on the server.
_Avoid_: Admin user, root user, customer, client

**Panel**:
The web UI and API the Administrator uses. Holds no privileges over the system itself; asks Services to act.
_Avoid_: Dashboard, frontend, monolith

**Service**:
An independently running, independently failing component with one area of responsibility (core, backup, mail, database, notifier). A Service failing must not take down the Panel or other Services.
_Avoid_: Microservice, daemon, module

**Core Agent**:
The Service that owns Sites, Site Users, web serving and PHP Runtimes. The only Service that routinely needs full system privileges.
_Avoid_: Backend, worker

**Notifier**:
The Service that watches the health of the Panel and all other Services and tells the Administrator through Telegram and email. Independent of the Panel.
_Avoid_: Alerts module, monitor

## Sites

**Site**:
A web domain (or subdomain) served from its own directory, run as its own Site User, with one PHP Runtime and one Web Mode. Can be enabled or disabled.
_Avoid_: Website, domain, vhost, project

**Site User**:
The unprivileged operating-system user a Site runs and is accessed as. May log in over SSH with a key only.
_Avoid_: Account, owner

**SSH Key**:
A public key authorised for a Site User; the only way a Site User can log in over SSH. Added by pasting an existing public key, uploading a `.pub` file, or generating a pair in the Panel (private half shown once, never stored).
_Avoid_: Credential, password login

**PHP Runtime**:
One installed PHP version. Several Runtimes coexist; each Site picks exactly one and cannot affect Sites on another.
_Avoid_: PHP version (when meaning the installed thing), interpreter

**PHP Pool**:
The per-Site isolated PHP process group, running as the Site User on the Site's chosen PHP Runtime, with that Site's own settings and extensions.
_Avoid_: Worker, FPM instance

**Web Mode**:
Per-Site choice of how requests are served: nginx directly, or nginx in front of a local Apache. Nginx is always the public entry point.
_Avoid_: Web server choice, stack

## Data

**Database Server**:
An installed database engine (MariaDB or MySQL first; others installable on request). At most one version per engine.
_Avoid_: DB instance, cluster

**Database**:
A named database with its own user inside a Database Server, linked to a Site.
_Avoid_: Schema, DB

## Mail

**Mail Domain**:
A domain or subdomain that receives and sends mail on this server, with its own mailboxes and DKIM key. Independent of whether a Site exists on the same name.
_Avoid_: Mail zone, email domain

**Mailbox**:
One email address under a Mail Domain, with its own password.
_Avoid_: Account, email

**DNS Checklist**:
The set of DNS records the Administrator must create at their DNS provider for a Mail Domain to work, each with a live verified status. The Panel never edits DNS itself.
_Avoid_: DNS setup, DNS config

**Webmail Login**:
One-click, single-use, short-lived sign-in from the Panel into the webmail interface for a Mailbox, without the Panel storing the Mailbox password.
_Avoid_: SSO, auto-login

**Certificate**:
The free Let's Encrypt TLS certificate that lets a Site be served over HTTPS; issued on request, renewed automatically.
_Avoid_: SSL, cert

## Backups

**Backup Job**:
A scheduled definition of what to copy (a Site's files, its Databases, or both), where to put it, and how long to keep it.
_Avoid_: Backup task, backup plan

**Destination**:
Where a Backup Job stores copies: a local directory, a Borg repository, or an SFTP server.
_Avoid_: Target, storage, remote

**Restore**:
Bringing a backup back either over the live Site or into a separate location beside it.
_Avoid_: Rollback, recovery

## Operations

**Task**:
A long-running operation (installing a PHP Runtime or Database Server, a backup, issuing a certificate) started by the Administrator, with its own status and live log. Survives closing the browser; if interrupted by a restart it is marked interrupted and can be retried.
_Avoid_: Job (reserved for Backup Job), process

**File Manager**:
The Panel's file browser and editor for a Site's directory. Acts as the Site User, never as root, and cannot leave the Site's directory.
_Avoid_: Explorer, file browser

## Lifecycle

**Disabled Site**:
A Site taken out of service (visitors see a 503 page, its PHP Pool and scheduled tasks stop) while its data, Databases and SSH access stay intact.
_Avoid_: Suspended, paused, stopped
