#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOGFILE="$SCRIPT_DIR/photo-sorter.log"

echo "==> Stopping existing photo-sorter..."
pkill -f "photo-sorter serve" 2>/dev/null || true
sleep 1

echo "==> Building frontend..."
cd "$SCRIPT_DIR/web" && npm install --silent && npm run build

echo "==> Building backend..."
cd "$SCRIPT_DIR" && go build -o photo-sorter .

echo "==> Starting photo-sorter on port 8085..."
echo "==> Logs: tail -f $LOGFILE"
set -a && source "$SCRIPT_DIR/.env.dev" && set +a
exec "$SCRIPT_DIR/photo-sorter" serve 2>&1 | tee "$LOGFILE"
