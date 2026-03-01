#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOGFILE="$SCRIPT_DIR/photo-sorter.log"

# Parse flags
FORCE=false
for arg in "$@"; do
  case "$arg" in
    --force) FORCE=true ;;
  esac
done

echo "==> Stopping existing photo-sorter..."
pkill -f "photo-sorter serve" 2>/dev/null || true
sleep 1

# --- Smart caching ---

# 1. npm install: skip if node_modules/.package-lock.json is newer than package-lock.json
NEED_NPM=true
if [ "$FORCE" = false ] && [ -f "$SCRIPT_DIR/web/node_modules/.package-lock.json" ] && [ -f "$SCRIPT_DIR/web/package-lock.json" ]; then
  if [ "$SCRIPT_DIR/web/node_modules/.package-lock.json" -nt "$SCRIPT_DIR/web/package-lock.json" ]; then
    NEED_NPM=false
    echo "==> Dependencies unchanged, skipping npm install"
  fi
fi

if [ "$NEED_NPM" = true ]; then
  echo "==> Installing dependencies..."
  cd "$SCRIPT_DIR/web" && npm install --silent
  cd "$SCRIPT_DIR"
fi

# 2. Frontend build: skip if dist/index.html is newer than all source files
NEED_FRONTEND=true
DIST_INDEX="$SCRIPT_DIR/internal/web/static/dist/index.html"
if [ "$FORCE" = false ] && [ -f "$DIST_INDEX" ]; then
  CHANGED=$(find "$SCRIPT_DIR/web/src" "$SCRIPT_DIR/web/public" "$SCRIPT_DIR/web/index.html" \
    "$SCRIPT_DIR/web/vite.config.ts" "$SCRIPT_DIR/web/package.json" \
    -newer "$DIST_INDEX" 2>/dev/null | head -1)
  # Also check tsconfig files separately (glob doesn't work well in find args)
  if [ -z "$CHANGED" ]; then
    CHANGED=$(find "$SCRIPT_DIR/web" -maxdepth 1 -name 'tsconfig*.json' -newer "$DIST_INDEX" 2>/dev/null | head -1)
  fi
  # Also check if npm install just ran (new packages could affect build)
  if [ -z "$CHANGED" ] && [ "$NEED_NPM" = false ]; then
    NEED_FRONTEND=false
    echo "==> Frontend unchanged, skipping build"
  fi
fi

if [ "$NEED_FRONTEND" = true ]; then
  echo "==> Building frontend..."
  cd "$SCRIPT_DIR/web" && npm run build
  cd "$SCRIPT_DIR"
fi

# 3. Go build: skip if binary is newer than all .go files and dist/ wasn't rebuilt
NEED_GO=true
BINARY="$SCRIPT_DIR/photo-sorter"
if [ "$FORCE" = false ] && [ -f "$BINARY" ] && [ "$NEED_FRONTEND" = false ]; then
  CHANGED_GO=$(find "$SCRIPT_DIR" -name '*.go' -newer "$BINARY" -not -path '*/vendor/*' 2>/dev/null | head -1)
  CHANGED_GOMOD=$(find "$SCRIPT_DIR" -maxdepth 1 \( -name 'go.mod' -o -name 'go.sum' \) -newer "$BINARY" 2>/dev/null | head -1)
  if [ -z "$CHANGED_GO" ] && [ -z "$CHANGED_GOMOD" ]; then
    NEED_GO=false
    echo "==> Go code unchanged, skipping build"
  fi
fi

if [ "$NEED_GO" = true ]; then
  echo "==> Building backend..."
  cd "$SCRIPT_DIR" && go build -o photo-sorter .
fi

echo "==> Starting photo-sorter on port 8085..."
echo "==> Logs: tail -f $LOGFILE"
set -a && source "$SCRIPT_DIR/.env.dev" && set +a
exec "$SCRIPT_DIR/photo-sorter" serve 2>&1 | tee "$LOGFILE"
