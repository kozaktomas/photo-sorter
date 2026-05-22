# Photo Sorter

A self-contained photo management application: one Go binary, one Postgres
database (with pgvector), and an external CLIP/InsightFace embeddings
service. Photos are stored on disk in a deterministic `YYYY/MM/<filename>`
tree, with all metadata, albums, labels, faces, people, photo books, and
user accounts kept in Postgres.

> **Note:** This entire project was vibe-coded. Not a single line of code was written by a human — it's 100% AI-generated using [Claude Code](https://claude.ai/code).

## Features

- **Native upload pipeline** — JPEG/PNG/WebP/TIFF/GIF in pure Go; HEIC/HEIF via `heif-convert`; RAW (CR2/CR3/NEF/ARW/DNG/RAF/ORF/RW2/PEF/SRW) via `dcraw`. EXIF is read with `exiftool` (pure-Go fallback) and EXIF edits write an XMP sidecar next to the original.
- **AI-Powered Photo Analysis** — Analyze photos and generate labels, descriptions, and date estimates
- **Multiple AI Providers** — OpenAI, Google Gemini, Ollama, and llama.cpp
- **Batch Processing** — Process entire albums with optional batch API for 50% cost savings
- **Image Similarity Search** — Find similar photos using CLIP embeddings
- **Text-to-Image Search** — Search photos by text description with automatic Czech-to-English translation
- **Face Recognition** — Detect faces, find matches across the library, and assign people
- **Face Outlier Detection** — Find incorrectly assigned faces by computing distance from the per-person centroid
- **Photo Books** — Plan multi-format printed photo books with chapter color themes, customizable typography (24 free fonts), auto-generated table of contents, captions slots, and PDF export via LaTeX
- **Era Estimation** — Estimate photo time periods using CLIP embedding comparison
- **Duplicate Detection** — Find near-duplicate photos via pHash + CLIP embedding similarity
- **Trash with auto-purge** — Soft-delete photos to a per-user trash; an hourly daemon hard-deletes anything older than `TRASH_RETENTION_DAYS` (default 30)
- **EXIF edit** — Fix date, GPS, camera/lens/exposure, and EXIF text fields from the UI; changes also land in an XMP sidecar
- **Czech-aware full-text search** — Diacritic-folded `q=` filter on photo title/description/notes/file_name
- **Album Suggestions** — Find photos missing from albums via a pgvector centroid query
- **Photo Comparison** — Side-by-side photo comparison with metadata diff
- **Slideshow** — Full-screen photo slideshow with keyboard navigation
- **Mobile capture (PWA)** — `/capture` page that opens the device camera and uploads single shots
- **User management** — Native bcrypt accounts with roles `admin` / `editor` / `viewer`, first admin bootstrapped from env vars
- **Backup CLI** — `photo-sorter backup` tars the originals dir and `pg_dump`s the Postgres database in one timestamped folder, with retention
- **PhotoPrism migration** — One-shot `migrate-from-photoprism` + cell-by-cell `migrate-verify` for operators moving off PhotoPrism
- **MCP Server** — Model Context Protocol server for AI agent integration (52 tools)
- **Web Interface** — Browser-based UI with real-time progress updates via SSE
- **Internationalization** — Czech and English language support
- **Dry Run Mode** — Preview AI sort changes before applying them

## Requirements

- Go 1.26+
- Node.js 18+ (for the web UI)
- PostgreSQL 15+ with pgvector
- An embeddings service that exposes `POST /embed/image` and `POST /embed/text` (CLIP + InsightFace)
- `exiftool`, `heif-convert`, and `dcraw` on `PATH` for the upload pipeline (the Docker image bundles all three)

## Installation

```bash
# Clone the repository
git clone https://github.com/kozaktomas/photo-sorter.git
cd photo-sorter

# Build everything (frontend + Go binary)
make build

# Or build just the Go binary
make build-go
```

## Docker

Run Photo Sorter using Docker:

```bash
# Pull from GitHub Container Registry
docker pull ghcr.io/kozaktomas/photo-sorter:main

# Run with environment variables
docker run -p 8080:8080 \
  -e DATABASE_URL=postgres://user:pass@host:5432/photosorter?sslmode=disable \
  -e STORAGE_ORIGINALS_PATH=/data/originals \
  -e STORAGE_CACHE_PATH=/data/cache \
  -e BOOTSTRAP_ADMIN_USERNAME=admin \
  -e BOOTSTRAP_ADMIN_PASSWORD=change-me \
  -e EMBEDDING_URL=http://embeddings:8000 \
  -e OPENAI_TOKEN=sk-... \
  -v /path/to/data:/data \
  ghcr.io/kozaktomas/photo-sorter:main
```

Or build locally:

```bash
docker build -t photo-sorter .
docker run -p 8080:8080 --env-file .env photo-sorter
```

The image is automatically built and pushed to GHCR on every push to `main` and on version tags (`v*.*.*`).

## Configuration

Create a `.env` file in the project root:

```env
# PostgreSQL with pgvector (required)
DATABASE_URL=postgres://user:pass@localhost:5432/photosorter?sslmode=disable
DATABASE_MAX_OPEN_CONNS=25
DATABASE_MAX_IDLE_CONNS=5

# On-disk storage
STORAGE_ORIGINALS_PATH=/data/originals    # YYYY/MM/<filename>
STORAGE_CACHE_PATH=/data/cache            # thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg

# Bootstrap admin (consumed only on a fresh install)
BOOTSTRAP_ADMIN_USERNAME=admin
BOOTSTRAP_ADMIN_PASSWORD=change-me

# Trash retention
TRASH_RETENTION_DAYS=30

# Duplicate detection on upload
DUPLICATE_CHECK_ENABLED=true
DUPLICATE_PHASH_MAX_DIFF=8
DUPLICATE_EMBEDDING_MAX_DIST=0.05

# Embeddings service
EMBEDDING_URL=http://localhost:8000
EMBEDDING_DIM=768

# AI Providers (configure at least one for sort / text AI)
OPENAI_TOKEN=sk-...
GEMINI_API_KEY=...

# Local AI (optional)
OLLAMA_URL=http://localhost:11434
OLLAMA_MODEL=llama3.2-vision:11b
LLAMACPP_URL=http://localhost:8080

# Web server
WEB_PORT=8080
WEB_HOST=0.0.0.0
WEB_SESSION_SECRET=change-me-in-production
WEB_ALLOWED_ORIGINS=https://photos.example.com
```

## Usage

### Sort Photos with AI

Analyze photos in an album and apply AI-generated labels:

```bash
# Preview changes without applying them
photo-sorter sort <album-uid> --dry-run

# Apply changes
photo-sorter sort <album-uid>

# Use Gemini instead of OpenAI
photo-sorter sort <album-uid> --provider gemini

# Use batch API for 50% cost savings (slower)
photo-sorter sort <album-uid> --batch

# Estimate date per photo instead of album-wide
photo-sorter sort <album-uid> --individual-dates

# Process with higher concurrency
photo-sorter sort <album-uid> --concurrency 10
```

### Album Management

```bash
# List all albums
photo-sorter albums

# Count photos in an album
photo-sorter count <album-uid>

# Move photos to a new album
photo-sorter move <source-album-uid> "New Album Name"

# Upload photos to an album
photo-sorter upload <album-uid> /path/to/photos
photo-sorter upload -r <album-uid> /path/to/photos  # recursive
```

### Label Management

```bash
# List all labels
photo-sorter labels

# Sort by photo count
photo-sorter labels --sort=-count

# Only show labels with at least 5 photos
photo-sorter labels --min-photos=5

# Delete labels
photo-sorter labels delete <uid1> <uid2>
```

### PostgreSQL Setup

Set up PostgreSQL with pgvector for storing embeddings and face data:

```bash
# Set up PostgreSQL with pgvector
docker run -d --name pgvector \
  -e POSTGRES_PASSWORD=secret \
  -p 5432:5432 \
  pgvector/pgvector:pg17

# Create database
docker exec -it pgvector psql -U postgres -c "CREATE DATABASE photosorter;"
```

Set `DATABASE_URL` in your `.env` file to connect to the database. Tables are automatically created on first startup.

### Photo Info

```bash
# Get info for a single photo
photo-sorter photo info <photo-uid>

# Get info for all photos in an album
photo-sorter photo info --album <album-uid> --json
```

### Cache Management

Backfill thumbnails and perceptual hashes after migrating or wiping the
cache:

```bash
# Generate every missing thumbnail
photo-sorter cache build-thumbs

# Backfill pHash + dHash for photos that lack them
photo-sorter cache compute-phashes
```

Compute CLIP era embedding centroids for photo era estimation:

```bash
# Compute and save era centroids
photo-sorter cache compute-eras
```

### Backups

Create a timestamped backup of the originals tree and the Postgres database (the thumbnail cache is intentionally excluded because it can be regenerated from the originals):

```bash
# Daily backup keeping the last 14 runs
photo-sorter backup --output /var/backups/photo-sorter --keep 14

# Originals only (no database access)
photo-sorter backup --output /tmp/bak --skip-db
```

Each run produces `<output>/photo-sorter-<YYYYMMDD-HHMMSS>/` containing `metadata.json`, `db.sql.zst` (or `.gz`), and `originals.tar.zst` (or `.tar.gz`). The directory is written atomically — partial runs leave a `.photo-sorter-<ts>.tmp/` directory for inspection unless `--cleanup-on-failure` is set.

Requires `pg_dump` on `PATH` (`apt: postgresql-client`; the Docker image already ships it).

#### Schedule with systemd

Sample units live in [`deploy/systemd/`](deploy/systemd/). Install with:

```bash
sudo cp deploy/systemd/photo-sorter-backup.{service,timer} /etc/systemd/system/
sudo mkdir -p /etc/photo-sorter
sudo tee /etc/photo-sorter/backup.env <<EOF
STORAGE_ORIGINALS_PATH=/data/originals
DATABASE_URL=postgres://user:pass@host:5432/photosorter?sslmode=disable
EOF
sudo mkdir -p /var/backups/photo-sorter
sudo systemctl daemon-reload
sudo systemctl enable --now photo-sorter-backup.timer
```

The timer fires the oneshot service nightly at 03:00 (with a 10-minute random jitter, persistent across reboots).

#### Backup & Restore runbook

For the full end-to-end procedure — what to back up, what to skip,
restore steps, and disaster-recovery checklist — see
[`docs/backup.md`](docs/backup.md). The TL;DR is: archive the originals
tree (rsync/borg) and dump Postgres (`photo-sorter db-export`); on
restore, `db-import` the dump, point `STORAGE_ORIGINALS_PATH` at the
restored tree, and let `cache build-thumbs` regenerate the thumbnail
cache on demand.

### Migrating from PhotoPrism

If you are coming from an existing PhotoPrism install, photo-sorter can
import its MariaDB database and primary originals directly:

```bash
# Dry-run: walk the source, print counts, no DB writes
photo-sorter migrate-from-photoprism \
  --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
  --pp-originals /photoprism/originals \
  --uploader-username admin \
  --dry-run

# Full migration
photo-sorter migrate-from-photoprism \
  --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
  --pp-originals /photoprism/originals \
  --uploader-username admin

# Cell-by-cell verification (zero diffs = safe to drop PhotoPrism)
photo-sorter migrate-verify \
  --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
  --pp-originals /photoprism/originals
```

UIDs (photos, albums, subjects, markers) are preserved verbatim so cached
references in `embeddings`, `faces`, `section_photos`, and `page_slots`
stay valid without a remap pass. See
[`docs/migration-from-photoprism.md`](docs/migration-from-photoprism.md)
for the full runbook.

### Web Interface

Start the web server for browser-based access:

```bash
# Production (uses embedded frontend)
photo-sorter serve

# Custom port
photo-sorter serve --port 3000
```

The web UI requires authentication. Log in with the bootstrap admin you
created via `BOOTSTRAP_ADMIN_USERNAME` / `BOOTSTRAP_ADMIN_PASSWORD` on
first start, then create additional users from **Settings → Users** (admin
only).

For development with hot reload:

```bash
# Terminal 1: Frontend dev server
make dev-web

# Terminal 2: Go backend
make dev-go
```

## Architecture

```
photo-sorter/
├── cmd/                    # CLI commands (Cobra)
├── internal/
│   ├── ai/                 # AI provider implementations
│   │   └── prompts/        # Embedded prompt templates
│   ├── auth/               # Password hashing + bootstrap admin
│   ├── config/             # Configuration and pricing
│   ├── constants/          # Shared constants (page sizes, thresholds)
│   ├── database/           # PostgreSQL+pgvector storage backend
│   ├── exif/               # EXIF reader + XMP sidecar writer
│   ├── facematch/          # Face matching utilities (IoU, bbox conversion)
│   ├── fingerprint/        # Perceptual hashing and embeddings
│   ├── imgconvert/         # HEIC/RAW → JPEG via heif-convert / dcraw
│   ├── latex/              # PDF export via LaTeX
│   ├── mcp/                # MCP server for AI agent integration
│   ├── migrate/            # PhotoPrism→native one-shot migrator
│   ├── photopipe/          # Upload pipeline (hash → dedup → EXIF → store → thumbs)
│   ├── photoprism/         # PhotoPrism REST client (used only by migration)
│   ├── sorter/             # Photo analysis orchestration
│   ├── storage/            # On-disk layout for originals + thumbnail cache
│   ├── thumb/              # Thumbnail registry + GenerateSizes
│   ├── trash/              # Soft-delete trash + hourly auto-purge daemon
│   ├── verify/             # migrate-verify field-level comparator
│   └── web/                # Web server and API handlers
└── web/                    # React + TypeScript frontend
```

### Data Flow (upload)

1. Multipart upload arrives at `POST /api/v1/upload`.
2. `internal/photopipe` hashes the file (SHA256), detects the format, and skips exact duplicates by hash.
3. HEIC/RAW are funnelled through `imgconvert` to a JPEG intermediate. EXIF is read via `exiftool` (pure-Go fallback).
4. The near-duplicate scan (pHash + CLIP embedding) runs when enabled.
5. The original is written to `STORAGE_ORIGINALS_PATH/YYYY/MM/<basename>`; rows land in `photos`, `photo_files`, `photo_phashes`.
6. `internal/thumb.GenerateSizes` decodes the source once and writes every registered thumbnail size under `STORAGE_CACHE_PATH/thumb/...`.
7. Optionally the embeddings service is called to populate `embeddings` + `faces` (also reachable via the Process job).

## Documentation

- [Architecture](docs/architecture.md) - System design, package structure, and data flow
- [CLI Reference](docs/cli-reference.md) - Complete reference for all CLI commands
- [Web UI Guide](docs/web-ui.md) - Guide to the web interface features
- [API Reference](docs/API.md) - REST API documentation
- [Similarity Search](docs/similarity-search.md) - pgvector cosine search: indexes, ef_search, maintenance
- [Face Markers](docs/markers.md) - Marker system and face-to-marker matching
- [Era Estimation](docs/era-estimation.md) - Era estimation using CLIP embeddings
- [Photo Books](docs/photo-book.md) - Photo book planning tool
- [Migration from PhotoPrism](docs/migration-from-photoprism.md) - One-shot import runbook
- [Backup & Restore](docs/backup.md) - Operator runbook for backing up and restoring a photo-sorter install
- [Testing Environment](docs/testing-environment.md) - Dev/test environment setup

## Development

```bash
# Run full quality gate (fmt + vet + lint + test)
make check

# Format Go code
make fmt

# Run go vet
make vet

# Run tests (with race detector)
make test

# Run tests with verbose output
make test-v

# Lint Go code
make lint

# Build frontend only
make build-web

# Lint frontend
make web-lint

# Clean build artifacts
make clean
```

## Troubleshooting

### Frontend build fails with "Cannot find module @rollup/rollup-..."

This is a [known npm bug](https://github.com/npm/cli/issues/4828) with optional dependencies. The `package-lock.json` contains platform-specific binaries that may not match your system.

**Solution:** Delete `node_modules` and `package-lock.json`, then reinstall:

```bash
cd web
rm -rf node_modules package-lock.json
npm install
```

## License

MIT
