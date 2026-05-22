#!/bin/sh
set -e

groupadd --system photo-sorter || true
useradd --system -d /var/lib/photo-sorter -s /usr/sbin/nologin -g photo-sorter photo-sorter || true

chown root:photo-sorter /etc/photo-sorter/photo-sorter.env
chmod 640 /etc/photo-sorter/photo-sorter.env

mkdir -p /var/lib/photo-sorter/originals /var/lib/photo-sorter/cache
chown -R photo-sorter:photo-sorter /var/lib/photo-sorter

fc-cache -f >/dev/null || true

systemctl daemon-reload
systemctl enable photo-sorter || true

cat <<'EOF'

─────────────────────────────────────────────────────────────
 photo-sorter installed.

 Next steps:
   1. Edit /etc/photo-sorter/photo-sorter.env and set at minimum:
        - DATABASE_URL
        - WEB_SESSION_SECRET
   2. Start the service:
        sudo systemctl start photo-sorter
   3. Browse to http://<host>:8080
─────────────────────────────────────────────────────────────

EOF
