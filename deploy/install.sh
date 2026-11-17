#!/usr/bin/env bash
# Installs Hosting Panel on Ubuntu 24.04+. Run as root from the unpacked dist/ directory:
#   ./install.sh panel.example.com
set -euo pipefail
[ "$(id -u)" = 0 ] || { echo "run as root"; exit 1; }
DOMAIN="${1:?usage: install.sh <panel-domain>}"
HERE="$(cd "$(dirname "$0")" && pwd)"
. /etc/os-release
case "$VERSION_ID" in 24.04|25.*|26.*) ;; *) echo "Ubuntu 24.04-26.x required (found $VERSION_ID)"; exit 1;; esac

export DEBIAN_FRONTEND=noninteractive
echo 'Acquire::Retries "5";' > /etc/apt/apt.conf.d/80hp-retries
apt-get update
apt-get -y install software-properties-common ca-certificates
add-apt-repository -y ppa:ondrej/php
apt-get update
# The panel itself needs PHP 8.4+ (Laravel 13); sites use whatever runtimes the Core Agent installs.
apt-get -y install nginx certbot python3-certbot-nginx php8.4-fpm php8.4-cli php8.4-sqlite3 php8.4-mbstring php8.4-xml php8.4-curl php8.4-zip php8.4-intl composer unzip sqlite3 rsync

# Users and shared socket group.
getent group hostingpanel >/dev/null || groupadd --system hostingpanel
id hostingpanel >/dev/null 2>&1 || useradd --system -g hostingpanel -d /opt/hostingpanel -s /usr/sbin/nologin hostingpanel
id hostingpanel-notifier >/dev/null 2>&1 || useradd --system -g hostingpanel -s /usr/sbin/nologin hostingpanel-notifier
install -m 0644 "$HERE/deploy/hostingpanel.tmpfiles.conf" /etc/tmpfiles.d/hostingpanel.conf
systemd-tmpfiles --create
mkdir -p /var/www/letsencrypt /etc/hostingpanel /var/lib/hostingpanel /var/log/hostingpanel/sites /srv/sites
chmod 0711 /srv/sites

# Binaries.
install -m 0755 "$HERE"/bin/* /usr/local/bin/

# Panel.
mkdir -p /opt/hostingpanel
rsync -a --delete --exclude .env --exclude storage/ --exclude 'database/*.sqlite' "$HERE/panel/" /opt/hostingpanel/panel/
cd /opt/hostingpanel/panel
mkdir -p storage/framework/{cache,sessions,views} storage/logs bootstrap/cache database
COMPOSER_ALLOW_SUPERUSER=1 composer install --no-dev --optimize-autoloader --no-interaction
if [ ! -f .env ]; then
  cp .env.example .env
  sed -i "s#^APP_ENV=.*#APP_ENV=production#; s#^APP_DEBUG=.*#APP_DEBUG=false#; s#^APP_URL=.*#APP_URL=https://$DOMAIN#; s#^DB_CONNECTION=.*#DB_CONNECTION=sqlite#" .env
  sed -i '/^DB_DATABASE=/d;/^DB_HOST=/d;/^DB_PORT=/d;/^DB_USERNAME=/d;/^DB_PASSWORD=/d' .env
fi
grep -q '^APP_KEY=base64:' .env || php artisan key:generate --force
touch database/database.sqlite
php artisan migrate --force
# Caches must be built after APP_KEY exists: Livewire's route prefix is derived from it.
php artisan optimize:clear
php artisan config:cache && php artisan route:cache && php artisan view:cache
chown -R hostingpanel:hostingpanel /opt/hostingpanel

# Panel web: its own php-fpm pool and nginx vhost.
rm -f /etc/php/8.4/fpm/pool.d/hostingpanel-panel.conf
install -m 0644 "$HERE/deploy/panel-fpm.conf" /etc/hostingpanel/panel-fpm.conf
install -m 0644 "$HERE/deploy/hostingpanel-panel-fpm.service" /etc/systemd/system/hostingpanel-panel-fpm.service
sed "s/PANEL_DOMAIN/$DOMAIN/g" "$HERE/deploy/panel.nginx.conf" > /etc/nginx/sites-available/hostingpanel-panel.conf
ln -sf /etc/nginx/sites-available/hostingpanel-panel.conf /etc/nginx/sites-enabled/hostingpanel-panel.conf
rm -f /etc/nginx/sites-enabled/default
# Site users may log in over SSH by key only; the Core Agent writes the drop-in, nothing to do here.
nginx -t
systemctl daemon-reload
systemctl enable hostingpanel-panel-fpm
systemctl restart hostingpanel-panel-fpm
systemctl reload nginx
# Re-running the installer rewrites the vhost as plain HTTP; put an existing certificate back.
if [ -d "/etc/letsencrypt/live/$DOMAIN" ]; then
  certbot install --nginx --cert-name "$DOMAIN" --redirect --non-interactive || echo "WARNING: could not re-apply the HTTPS certificate; run: certbot --nginx -d $DOMAIN"
fi

# Services.
if [ ! -f /etc/hostingpanel/notifier.json ]; then
  # Watches every Service and the Panel. Add "telegram" / "smtp" blocks (see deploy/notifier.example.json) to receive alerts.
  cat > /etc/hostingpanel/notifier.json <<JSON
{
  "interval_seconds": 30,
  "fail_threshold": 2,
  "disk_percent": 90,
  "checks": [
    { "name": "Panel", "kind": "http", "target": "http://$DOMAIN/login" },
    { "name": "Panel PHP", "kind": "systemd", "target": "hostingpanel-panel-fpm" },
    { "name": "Core service", "kind": "systemd", "target": "hostingpanel-core" },
    { "name": "Database service", "kind": "systemd", "target": "hostingpanel-dbmanager" },
    { "name": "Backup service", "kind": "systemd", "target": "hostingpanel-backup" },
    { "name": "Mail service", "kind": "systemd", "target": "hostingpanel-mail" },
    { "name": "nginx", "kind": "systemd", "target": "nginx" }
  ]
}
JSON
  chgrp hostingpanel /etc/hostingpanel/notifier.json; chmod 0640 /etc/hostingpanel/notifier.json
fi
GID="$(getent group hostingpanel | cut -d: -f3)"
for u in core dbmanager backup mail notifier; do
  sed "s/%GID%/$GID/" "$HERE/deploy/hostingpanel-$u.service" > "/etc/systemd/system/hostingpanel-$u.service"
done
systemctl daemon-reload
systemctl enable --now hostingpanel-core hostingpanel-dbmanager hostingpanel-backup hostingpanel-mail hostingpanel-notifier

cat <<MSG

Installed. Next:
  1. Create the administrator:   cd /opt/hostingpanel/panel && sudo -u hostingpanel php artisan panel:admin you@example.com
     (it prints the TOTP secret to add to your authenticator app)
  2. Open http://$DOMAIN and log in.
  3. Turn on HTTPS for the panel:   certbot --nginx -d $DOMAIN
  4. Edit /etc/hostingpanel/notifier.json (Telegram / SMTP) and: systemctl restart hostingpanel-notifier
MSG
