#!/bin/sh
# Dev stack inside one Linux container: all Services in dry-run + the Panel on :8200.
set -e
mkdir -p /run/hostingpanel /tmp/hp/state /tmp/hp/root
for b in core-agent dbmanager backup notifier mail-service system-service; do [ -x /bin/hp/$b ] || echo "missing $b"; done
/bin/hp/core-agent -socket /run/hostingpanel/core.sock -db /tmp/hp/state/core.db -dry-run-root /tmp/hp/root >/tmp/hp/core.log 2>&1 &
/bin/hp/dbmanager -socket /run/hostingpanel/db.sock -db /tmp/hp/state/db.db -dry-run >/tmp/hp/db.log 2>&1 &
/bin/hp/backup -socket /run/hostingpanel/backup.sock -state /tmp/hp/state/backup -dry-run >/tmp/hp/backup.log 2>&1 &
/bin/hp/mail-service -socket /run/hostingpanel/mail.sock -sso-socket /run/hostingpanel/sso.sock -state /tmp/hp/state/mail -mail-root /tmp/hp/mail -dry-run >/tmp/hp/mail.log 2>&1 &
[ -x /bin/hp/system-service ] && /bin/hp/system-service -socket /run/hostingpanel/system.sock -state /tmp/hp/state/system -dry-run >/tmp/hp/system.log 2>&1 &
cd /app
[ -f database/database.sqlite ] || touch database/database.sqlite
php artisan migrate --force >/dev/null 2>&1 || true
exec php artisan serve --host=0.0.0.0 --port=8200
