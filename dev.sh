#!/bin/bash
set -e

LOGFILE=/app/photo-sorter.log

echo "==> Stopping existing photo-sorter..."
pkill -f "photo-sorter serve" 2>/dev/null || true
sleep 1

echo "==> Building frontend..."
cd /app/web && npm install --silent && npm run build

echo "==> Building backend..."
cd /app && go build -o photo-sorter .

echo "==> Starting photo-sorter on port 8085..."
echo "==> Logs: tail -f $LOGFILE"
set -a && source /app/.env.dev && set +a
exec /app/photo-sorter serve 2>&1 | tee $LOGFILE
