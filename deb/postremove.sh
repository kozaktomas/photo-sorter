#!/bin/sh
set -e
systemctl daemon-reload || true
if [ "$1" = "purge" ]; then
  # Remove regenerable state on purge, but never originals (user data).
  rm -rf /var/lib/photo-sorter/cache
  rm -rf /etc/photo-sorter
  userdel  photo-sorter 2>/dev/null || true
  groupdel photo-sorter 2>/dev/null || true
  echo "Note: /var/lib/photo-sorter/originals/ was preserved. Remove manually if no longer needed."
fi
