# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview

Photo-sorter is a self-contained photo management application: one Go binary,
one Postgres database (with pgvector), and an external embeddings service.
PostgreSQL is the single source of truth for photos, albums, labels, faces,
subjects, markers, photo books, and user accounts.

### Storage layout

Originals live on disk under `STORAGE_ORIGINALS_PATH` in the layout
`YYYY/MM/<filename>` (same on-disk shape as PhotoPrism — an existing
PhotoPrism library can be migrated in place without renaming). The
thumbnail cache lives under `STORAGE_CACHE_PATH/thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg`,
where `<hash>` is the photo's SHA256 and `<aa>/<bb>/<cc>` are the first
three byte-pair shards of the hash. The thumbnail cache is regenerable
from the originals via `cache build-thumbs`; backups intentionally skip it.

### Backup & disaster recovery

**Backup CLI: partially delivered.** The DB side is covered by
`photo-sorter db-export` / `photo-sorter db-import` (this command pair
dumps and restores the entire `photosorter` Postgres database via
`pg_dump` / `pg_restore`). Archival of the originals tree is left to the
operator — use `rsync`, `borg`, or the higher-level `photo-sorter
backup` command (which bundles both into a timestamped directory). After
a `db-import` on a new host, restart the server and re-run
`cache build-thumbs` to regenerate any missing thumbnail sizes from the
synced originals.

### Users + bootstrap

Users authenticate against the local `users` table (bcrypt-hashed
passwords) with roles `admin` / `editor` / `viewer`. The first admin is
created automatically from `BOOTSTRAP_ADMIN_USERNAME` + `BOOTSTRAP_ADMIN_PASSWORD`
on a fresh install (no-op once any user exists). Subsequent users are
managed via `/api/v1/users/*` (admin only) or the Settings page.

### Upload pipeline

`internal/photopipe` ingests JPEG/PNG/WebP/TIFF/GIF natively and shells
out to `heif-convert` for HEIC/HEIF and `dcraw` for RAW
(CR2/CR3/NEF/ARW/DNG/RAF/ORF/RW2/PEF/SRW). EXIF is read via `exiftool`
(with a pure-Go fallback); EXIF edits also write an XMP sidecar via
`exiftool`. The official Docker image bundles `dcraw_emu` (Alpine's
LibRaw replacement, wrapped by `scripts/dcraw-shim.sh` installed as
`/usr/local/bin/dcraw`), `libheif-tools`, and `exiftool`. The `serve`
command logs a startup `WARN` line for each missing binary so
deployments fail loud, not silent.

### Migration from PhotoPrism (historical)

Operators with an existing PhotoPrism instance can import it with
`photo-sorter migrate-from-photoprism` (read PhotoPrism's MariaDB and
copy originals) followed by `photo-sorter migrate-verify` (cell-by-cell
diff with tolerance bands; `--strict` disables them). A clean
`migrate-verify` is the gate for dropping PhotoPrism + MariaDB. UIDs are
preserved verbatim across photos, albums, subjects, and markers, so
cached references in `embeddings`, `faces`, `section_photos`, and
`page_slots` stay valid without a remap pass. See
[`docs/migration-from-photoprism.md`](docs/migration-from-photoprism.md)
for the full runbook. The `internal/photoprism/` REST client is retained
only to drive the migration commands.

## Browser

Chromium is available for headless browsing (e.g. checking web UI output):
```bash
chromium --headless --no-sandbox --dump-dom http://localhost:8085
chromium --headless --no-sandbox --screenshot=/tmp/screenshot.png --window-size=1280,800 http://localhost:8085
```

## Denied Files

Do NOT read or access the following files:
- `.envrc` - Contains sensitive environment variables

## Pre-commit Requirements

A pre-commit hook runs automatically on `git commit`. Before committing, ensure:

- **Go changes:** `make lint` must pass
- **Frontend changes:** `npx tsc --noEmit` and `npm run lint` (in `web/`) must pass

The hook only runs checks relevant to the files being committed.

## Build and Test Commands

```bash
# Build everything (frontend + Go binary)
make build

# Build only Go binary (without frontend)
make build-go

# Build only frontend
make build-web

# Run full quality gate (fmt + vet + lint + test)
make check

# Format Go code (goimports + go fmt)
make fmt

# Run go vet
make vet

# Run tests with race detector (explicit paths to avoid root-owned volumes/ directory)
make test

# Run tests with verbose output
make test-v

# Run a single test
go test -v ./internal/photoprism/ -run TestGetAlbum

# Lint Go code
make lint

# Lint and auto-fix
make lint-fix

# Install all book typography fonts to /usr/local/share/fonts/photo-sorter
# (one-time setup for dev environments running outside Docker; uses sudo,
# idempotent). System path is required because the lualatex subprocess in
# internal/latex/latex.go overrides HOME to a temp dir, which hides any
# user-local font directory from fontconfig.
# Uses scripts/install-fonts.sh — the same script the Docker build runs.
make install-fonts

# Run the CLI
go run . <command>

# Start the web server
go run . serve

# Dump the Postgres database to a single file (originals tree NOT included).
go run . db-export -o photosorter-snapshot.dump

# Restore a database dump produced by db-export (skipping the prompt).
go run . db-import -i photosorter-snapshot.dump --yes

# Local snapshot build of the .deb (no publish). Produces
# dist/photo-sorter_*_linux_{amd64,arm64}.deb and matching .tar.gz
# archives. Requires goreleaser on PATH and internet (the fonts
# staging step downloads from CTAN / Google Fonts / GitHub releases).
goreleaser release --snapshot --clean --skip=publish
```

### Version Injection

Build metadata (`Version`, `CommitSHA` in `cmd/version.go`) is injected via `-ldflags` at compile time. The Makefile auto-detects the current commit hash. In Docker builds, GitHub Actions computes the version (tag name or `dev`) and passes it as build args. The version is exposed via `GET /api/v1/config` and displayed in the web UI header next to the GitHub icon.

## Development Environment

**IMPORTANT:** After every code change, run the dev script to rebuild and restart the server:

```bash
./dev.sh          # Smart rebuild (skips unchanged steps)
./dev.sh --force  # Force full rebuild (bypass caching)
```

This script:
1. Stops any running photo-sorter process
2. Runs `npm install` (skipped if `node_modules` is up-to-date with `package-lock.json`)
3. Builds the frontend via `tsc -b && vite build` (skipped if `dist/` is newer than all source files)
4. Builds the Go binary (skipped if binary is newer than all `.go` files and frontend wasn't rebuilt)
5. Starts the server on port 8085 (configurable via `PORT` env variable) backed by the pgvector test service

Smart caching makes repeated runs fast (~5s when nothing changed vs ~10min for full rebuild on the Pi).

To check server logs:
```bash
tail -f /app/photo-sorter.log
```

The dev environment uses:
- PostgreSQL: `pgvector:5432` — the only managed service in `docker-compose.yml` (credentials come from `.env.dev`)
- Embeddings (CLIP + faces): external, configured via `EMBEDDING_URL` in `.env.dev`
- Originals tree: `STORAGE_ORIGINALS_PATH` (defaults to `./data/originals`) — `YYYY/MM/<filename>`
- Thumbnail cache: `STORAGE_CACHE_PATH` (defaults to `./data/cache`) — `thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg`
- External decoders the upload pipeline shells out to: `exiftool`, `heif-convert` (libheif-tools), `dcraw` (LibRaw shim). Install via the OS package manager for dev; production reads them from the Docker image.

**Book typography fonts:** PDF export requires the book fonts to be installed
on the host (production reads them from the Docker image's `/usr/share/fonts`).
For dev environments, run `make install-fonts` once after cloning — it sudo-
installs all 24 free fonts to `/usr/local/share/fonts/photo-sorter` using the
same `scripts/install-fonts.sh` the Docker build calls. The system path
(rather than `~/.local/share/fonts`) is mandatory: `compileLatex` in
`internal/latex/latex.go` overrides `HOME` to a fresh temp dir before
spawning lualatex (so luaotfload writes its cache there), which hides any
user-local font directory from fontconfig. Bookman Old Style is proprietary
and is not installed automatically; see the script header for manual
installation instructions. `dev.sh` warns if the canonical sentinel font is
missing.

## API auth (curl/Playwright)

Photo-sorter exposes a cookie-based session. Log in once and reuse the
`session` cookie for subsequent calls:

```bash
curl -c cookies.txt -X POST http://localhost:8085/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"your-password"}'
curl -b cookies.txt "http://localhost:8085/api/v1/albums"
```

The bootstrap admin credentials come from `BOOTSTRAP_ADMIN_USERNAME` /
`BOOTSTRAP_ADMIN_PASSWORD` on the first run; additional users are managed
via `/api/v1/users/*` (admin only).

## Architecture

A single Go binary (Cobra CLI + Chi HTTP router) backed by PostgreSQL and
an external embeddings service. The frontend is React + TypeScript +
TailwindCSS, embedded into the binary at compile time via `go:embed`.

### Core Components

- **cmd/** — Cobra commands (sort, albums, labels, count, move, upload, photo, cache, serve, backup, migrate-from-photoprism, migrate-verify, migrate-remap-references, version)
- **internal/ai/** — AI provider interface with OpenAI, Gemini, Ollama, and llama.cpp implementations
- **internal/auth/** — Password hashing, role constants, bootstrap admin creation
- **internal/config/** — Environment-based configuration loader
- **internal/constants/** — Shared constants (page sizes, thresholds, concurrency, upload limits)
- **internal/database/** — PostgreSQL storage with pgvector (HNSW indexes via `vector_cosine_ops`), repository interfaces, in-process readers/writers
- **internal/exif/** — EXIF reader (`exiftool` subprocess + pure-Go fallback) and XMP sidecar writer
- **internal/facematch/** — Face matching utilities (IoU, bbox conversion, name normalization)
- **internal/fingerprint/** — Perceptual hash computation (pHash, dHash) + embeddings HTTP client
- **internal/imgconvert/** — Format detection + thin wrappers around `heif-convert` (HEIC/HEIF) and `dcraw` (RAW), producing intermediate JPEGs
- **internal/latex/** — PDF export via LaTeX (markdown-to-LaTeX, layout validation, 12-column grid, font registry, templates). `GeneratePDFWithCallbacks` accepts an `ExportOptions.OnProgress` callback so the web job flow can emit SSE progress events.
- **internal/mcp/** — MCP server (HTTP SSE + Bearer auth) for AI agent integration
- **internal/migrate/** — One-shot PhotoPrism→native migrator (historical; used by `migrate-from-photoprism`)
- **internal/photopipe/** — Native upload pipeline: buffer → hash → format detect → exact-duplicate check → decode → EXIF → near-duplicate scan (pHash + embedding) → originals write → DB rows → thumbnails → pHash persist
- **internal/photoprism/** — PhotoPrism REST API client; retained only to drive the migration commands
- **internal/sorter/** — AI sort orchestration (photo fetch → AnalyzePhoto → label application)
- **internal/storage/** — On-disk layout for originals (`YYYY/MM/<filename>`) and the thumbnail cache (`thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg`)
- **internal/thumb/** — Thumbnail registry + `GenerateSizes` (decode once, resize many)
- **internal/trash/** — Soft-delete trash store and the hourly auto-purge daemon
- **internal/verify/** — Field-level `migrate-verify` comparator
- **internal/web/** — Web server (Chi router, REST handlers, SSE, embedded SPA)
- **web/** — React + TypeScript + TailwindCSS frontend (Vite, i18n with Czech + English)

### Data Flow (upload)

1. Multipart upload arrives at `POST /api/v1/upload` (or `/upload/job` for SSE).
2. `internal/photopipe` buffers to a temp file and SHA256-hashes it.
3. Format detection + exact-duplicate check (by SHA256). HEIC/RAW are decoded to JPEG via `imgconvert`.
4. EXIF is read with `exiftool` (pure-Go fallback). Near-duplicate scan (pHash + CLIP embedding) when enabled.
5. The original is written to `STORAGE_ORIGINALS_PATH/YYYY/MM/<basename>` and rows are inserted into the `photos`, `photo_files`, and `photo_phashes` tables.
6. `internal/thumb.GenerateSizes` writes every registered thumbnail size under the cache tree.
7. Optionally the embeddings service is called to populate `embeddings` + `faces` (Process job, can also run later).

### Configuration

Environment variables (loaded from `.env`):

| Variable | Required | Description |
|----------|----------|-------------|
| `DATABASE_URL` | Yes | PostgreSQL connection string with pgvector |
| `DATABASE_MAX_OPEN_CONNS` | No | Max open connections (default 25) |
| `DATABASE_MAX_IDLE_CONNS` | No | Max idle connections (default 5) |
| `STORAGE_ORIGINALS_PATH` | No | Originals root (default `/data/originals` in Docker, `./data/originals` in dev) — layout `YYYY/MM/<filename>` |
| `STORAGE_CACHE_PATH` | No | Cache root (default `/data/cache` in Docker, `./data/cache` in dev) — thumbnails live under `<CachePath>/thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg` |
| `BOOTSTRAP_ADMIN_USERNAME` | No* | Username for the first admin auto-created on a fresh install |
| `BOOTSTRAP_ADMIN_PASSWORD` | No* | Password for the first admin (warns and skips if either is unset and no users exist) |
| `TRASH_RETENTION_DAYS` | No | Retention window for the soft-delete trash (default 30). The hourly auto-purge daemon hard-deletes archived photos older than this. |
| `DUPLICATE_CHECK_ENABLED` | No | `true`/`false` global gate for the upload-time near-duplicate scan (default `true`) |
| `DUPLICATE_PHASH_MAX_DIFF` | No | Max hamming distance (0..64) between pHashes for the near-duplicate scan (default 8) |
| `DUPLICATE_EMBEDDING_MAX_DIST` | No | Max cosine distance (0..2) between CLIP embeddings for the near-duplicate scan (default 0.05) |
| `EMBEDDING_URL` | No | Embeddings service URL (default `http://localhost:8000`) |
| `EMBEDDING_DIM` | No | Embedding dimensions (default 768) |
| `OPENAI_TOKEN` | No** | OpenAI API key (sort, text check/rewrite/consistency, CLIP translate) |
| `GEMINI_API_KEY` | No** | Google Gemini API key |
| `OLLAMA_URL` | No | Ollama server URL (default `http://localhost:11434`) |
| `OLLAMA_MODEL` | No | Ollama model name (default `llama3.2-vision:11b`) |
| `LLAMACPP_URL` | No | llama.cpp server URL (default `http://localhost:8080`) |
| `LLAMACPP_MODEL` | No | llama.cpp model name (default `llava`) |
| `MCP_API_TOKEN` | No | Bearer token for MCP clients; when set, MCP is mounted at `/mcp/sse` on the `serve` command |
| `WEB_PORT` | No | Server port (default 8080) |
| `WEB_HOST` | No | Server host (default 0.0.0.0) |
| `WEB_SESSION_SECRET` | No | Secret for signing session cookies (warns at startup if unset) |
| `WEB_ALLOWED_ORIGINS` | No | Comma-separated CORS allowed origins (localhost is always allowed) |

*If both bootstrap vars are missing on a fresh install, the server logs a
WARN and starts anyway — the operator must create the first user manually.

**At least one AI provider must be configured for the sort command.

### AI Provider API Calls

Three modes: **standard** (N+1 API calls: N parallel AnalyzePhoto + 1 EstimateAlbumDate), **individual-dates** (N calls with date included in each), and **batch** (submit → poll → download results, 50% cheaper but slower).

### Sort Command Flags

```bash
go run . sort <album-uid> [flags]

Flags:
  --dry-run            Preview changes without applying them
  --limit N            Limit number of photos to process (0 = no limit)
  --individual-dates   Estimate date per photo instead of album-wide
  --batch              Use batch API for 50% cost savings (slower)
  --provider           AI provider: openai (default), gemini, ollama, llamacpp
  --force-date         Overwrite existing dates with AI estimates
  --concurrency N      Number of parallel requests in standard mode (default 5)
```

### Labels Command

```bash
go run . labels                          # List all labels (sorted by name)
go run . labels --sort=-count            # Sort by photo count (descending)
go run . labels --min-photos=5           # Only show labels with at least 5 photos
go run . labels delete <uid1> <uid2>     # Delete labels by UID
go run . labels delete --yes <uid>       # Delete without confirmation
```

### Upload Command

```bash
go run . upload <album-uid> <folder-path> [folder-path...] [flags]
  -r, --recursive      Search for photos recursively (default: flat search)
  -l, --label          Labels to apply to uploaded photos (can be repeated)
```

### Move Command

```bash
go run . move <source-album-uid> <new-album-name>
```

Moves all photos from a source album to a newly created album.

### Photo Commands

```bash
go run . photo info <photo-uid> [flags]      # Compute perceptual hashes (pHash, dHash)
  --album <uid>     Process all photos in an album
  --json            Output as JSON
  --limit N         Limit photos in album mode
  --concurrency N   Parallel workers (default 5)

go run . photo match <photo-uid>              # Find face matches for a photo
go run . photo similar <photo-uid>            # Find similar photos by embeddings
go run . photo clear-faces <photo-uid>        # Clear cached face data for a photo
```

### Cache Commands

```bash
go run . cache build-thumbs [flags]           # Backfill missing thumbnails (decode once, write every registered size)
  --concurrency N         Parallel workers (default 4)
  --sizes a,b,c           Subset of registered sizes
  --only-missing[=false]  Force regenerate existing thumbs when false (default true)
  --limit N               Cap photos processed
  --photo-uid UID         Backfill a single photo (overrides --limit)
  --json                  Output as JSON

go run . cache compute-phashes [flags]        # Backfill pHash + dHash for photos that lack them
  --limit N         Cap photos processed
  --concurrency N   Parallel workers (default 4)
  --json            Output as JSON

go run . cache compute-eras [flags]           # Compute CLIP era embedding centroids
  --dry-run   Preview without saving
  --json      Output as JSON
```

### MCP Server

The MCP server is integrated into the `serve` command. When `MCP_API_TOKEN` is set, MCP endpoints are mounted at `/mcp/sse` and `/mcp/message` on the same HTTP server. If the token is not set, MCP routes are simply not registered.

Exposes 52 MCP tools for photo book management, photo/album/label operations, and AI text tools over HTTP SSE. Server name: `photo-sorter-books`. MCP clients authenticate with `Authorization: Bearer <MCP_API_TOKEN>`.

Book-side MCP surface is at parity with the web book API for everything except heavy ops: `update_book` accepts the full typography payload (`body_font`, `heading_font`, `body_font_size`, `body_line_height`, `h1_font_size`, `h2_font_size`, `caption_opacity`, `caption_font_size`, `heading_color_bleed`, `caption_badge_size`, `body_text_pad_mm`, validated via `latex.ValidateFont` and the same numeric ranges as the web handler); `update_page` accepts `hide_page_number` for per-page folio suppression and a changed `section_id` triggers a full cross-section move via `BookWriter.MovePageToSection` (atomic, reconciles section photo pools, rejects targets in a different book with a clear error); `assign_captions_slot` routes the page's photo captions into a specific slot (at most one per page, maps `database.ErrCaptionsSlotExists` to a clear error). `auto_layout`, `preflight`, and the PDF export job flow are intentionally web-API-only.

**Package structure:**
```
internal/mcp/
├── server.go          # MCP server setup, Bearer auth middleware, handler
├── books.go           # Book and chapter tool handlers
├── sections.go        # Section and section photo tool handlers
├── pages.go           # Page and slot tool handlers
├── photos.go          # Photo metadata, thumbnails, similarity, text search
├── albums.go          # Album management tools
├── labels.go          # Label management tools
├── text.go            # AI text check, rewrite, consistency, version history
```

### Database Package

The `internal/database/` package provides storage for embeddings, faces data, photo books, text versions, and text check results using PostgreSQL with pgvector.

**Similarity search:** Every cosine query runs in pgvector. `embeddings.embedding` (768-dim CLIP) and `faces.embedding` (512-dim ResNet100) each have an HNSW index with operator class `vector_cosine_ops` (migration `038_pgvector_hnsw_indexes.sql`). Each query opens a small read-only transaction, runs `SET LOCAL hnsw.ef_search = 100`, and issues `ORDER BY embedding <=> $1::vector`. The constant lives in `internal/database/postgres/embeddings.go`. pgvector keeps the index up to date on INSERT/UPDATE/DELETE; the app holds no in-memory vector data. See [`docs/similarity-search.md`](docs/similarity-search.md).

**Key files:**
```
internal/database/
├── types.go            # StoredPhoto, StoredFace, ExportData, PhotoBook, BookChapter, BookSection, etc.
├── repository.go       # FaceReader, FaceWriter, EmbeddingReader, BookReader, BookWriter interfaces
├── provider.go         # Provider functions for getting readers/writers
├── cosine.go           # Cosine distance helper (used by face outlier ranking)
├── constants.go        # Shared constants (face size thresholds)
└── postgres/           # PostgreSQL backend
    ├── postgres.go     # Connection pool management
    ├── migrations.go   # Auto-apply migrations on startup
    ├── embeddings.go   # EmbeddingReader impl (pgvector, ef_search=100 + GetCentroid via AVG())
    ├── faces.go        # FaceReader/FaceWriter impl (pgvector)
    ├── era_embeddings.go  # EraEmbeddingReader/Writer implementation
    ├── books.go        # BookRepository (BookReader/BookWriter)
    ├── sessions.go     # Session persistence for web auth
    ├── text_versions.go   # TextVersionStore implementation
    ├── text_checks.go     # TextCheckStore implementation
    └── migrations/     # SQL migrations 001-038 (embedded)
```

**Tables:** `users` (admin/editor/viewer with bcrypt hashes), `photos`, `photo_files`, `albums` + `album_photos`, `labels` + `photo_labels`, `subjects`, `markers`, `photo_phashes`, `embeddings` (768-dim CLIP), `faces` (512-dim ResNet100 with cached marker metadata), `era_embeddings` (768-dim CLIP text centroids), `faces_processed` (tracking), `sessions` (with `user_uid` for upload support across restarts), `photo_books` (with typography settings: `body_font`, `heading_font`, `body_font_size`, `body_line_height`, `h1_font_size`, `h2_font_size`, `caption_opacity`, `caption_font_size`, `heading_color_bleed` added in migrations 021-023, plus `body_text_pad_mm` in migration 029), `book_chapters` (migration 016, with `color` column from migration 020), `book_sections` (with optional `chapter_id`), `section_photos`, `book_pages` (with `split_position`, `hide_page_number` from migration 025, `1_fullbleed` format added to the CHECK constraint in migration 027), `page_slots` (with `text_content`, `is_captions_slot` from migration 026, `is_contents_slot` from migration 030, `crop_x`/`crop_y`/`crop_scale`; photo_uid / text_content / is_captions_slot / is_contents_slot are mutually exclusive), `text_versions` (migration 017), `text_check_results` (migration 019, extended by migration 028 with a `suggestions JSONB` column), `album_share_links` (migration 039 — public-share slugs keyed on `slug PRIMARY KEY`, FK to `albums(uid)` ON DELETE CASCADE, optional bcrypt `password_hash` and `expires_at`).

**Face name normalization:** `GetFacesBySubjectName` normalizes names via `facematch.NormalizePersonName` (remove diacritics, lowercase, dashes→spaces) using the `unaccent` PostgreSQL extension.

### AI Prompts

Located in `internal/ai/prompts/` (embedded at compile time):
- `photo_analysis.txt` - Labels + description only
- `photo_analysis_with_date.txt` - Labels + description + date estimation
- `album_date.txt` - Album-wide date estimation from descriptions
- `clip_translate.txt` - Czech to CLIP-optimized English translation for text search
- `text_check.txt` - Czech text spelling, diacritics, grammar checking, and advisory readability suggestions (severity: `major` for hard-to-read text, `minor` for polish tips)
- `text_rewrite.txt` - Czech text length adjustment (shorter/longer)
- `text_consistency.txt` - Czech text style consistency analysis across book texts

**Language:** Czech (descriptions are generated in Czech)

**Location context:** Prompts assume photos are from Veselice, Czech Republic (Jihomoravský kraj, near Moravský kras)

**Metadata:** Prompts instruct AI to use provided metadata (filename, EXIF date, GPS) for better analysis

### Pricing Configuration

Model prices are in `internal/config/prices.yaml` (embedded at compile time). Supports per-model standard and batch pricing for gpt-4.1-mini (photo analysis + CLIP translate + sort), gpt-5.4-mini (text check / rewrite / consistency — uses `max_completion_tokens` instead of `max_tokens`), gemini-2.5-flash, llama3.2-vision (Ollama), and llava (llama.cpp). The single source of truth for the text operations model is `ai.TextModel` in `internal/ai/text.go`, referenced by both the web handler (`internal/web/handlers/text.go`) and the MCP handler (`internal/mcp/text.go`).

### Metadata Behavior

When applying AI results to a photo:
- **Labels:** Replaced with AI-suggested labels (confidence > 80%)
- **Description/Caption:** Always regenerated (includes AI model info)
- **Date (TakenAt):** Only set if photo has no existing date (Year = 0 or 1), unless `--force-date` is used
- **Notes:** Updated with "Analyzed by: <model>"

Existing EXIF dates are preserved — AI date estimation only fills gaps. Use `--force-date` to overwrite incorrect dates.

### Web UI

The web UI provides browser-based access to all CLI functionality. It uses React + TypeScript + TailwindCSS for the frontend and Chi router for the backend.

```bash
# Start the web server (production - uses embedded frontend)
go run . serve

# Start with custom port
go run . serve --port 3000

# Development mode (run in separate terminals)
make dev-web   # Start Vite dev server with hot reload
make dev-go    # Start Go server
```

**Environment Variables:**
- `WEB_PORT` - Server port (default: 8080)
- `WEB_HOST` - Server host (default: 0.0.0.0)
- `WEB_SESSION_SECRET` - Secret for signing session cookies (warns at startup if unset)
- `WEB_ALLOWED_ORIGINS` - Comma-separated CORS allowed origins (localhost always allowed)

Sessions are persisted to PostgreSQL (`sessions` table) for survival across server restarts.

Session cookies use `HttpOnly`, `SameSite=Strict`, and auto-detect `Secure` flag when behind HTTPS (`X-Forwarded-Proto` or direct TLS). Security headers (CSP, X-Content-Type-Options, X-Frame-Options) are set on all responses.

**API Endpoints:**
- `GET /api/v1/health` - Health check (no auth)
- `POST /api/v1/auth/login` - Login with the local user account (bcrypt-hashed password against the `users` table)
- `POST /api/v1/auth/logout` - Logout
- `GET /api/v1/auth/status` - Check authentication status
- `GET /api/v1/albums` - List albums
- `POST /api/v1/albums` - Create album
- `GET /api/v1/albums/{uid}` - Get single album
- `GET /api/v1/albums/{uid}/photos` - Get photos in album
- `POST /api/v1/albums/{uid}/photos` - Add photos to album
- `DELETE /api/v1/albums/{uid}/photos` - Remove photos from album
- `DELETE /api/v1/albums/{uid}/photos/batch` - Remove specific photos from album (batch)
- `POST /api/v1/albums/{uid}/share` - Mint a public share link (HasWriteAccess). Body: `{ slug?, password?, expires_at? (RFC3339) }`. `slug` defaults to a slugified album title with `-N` dedup suffixes (must match `^[a-z0-9-]{3,64}$`); `password` is bcrypt-hashed and the raw value is never persisted. Returns the share record (`has_password` bool, never the hash).
- `GET /api/v1/albums/{uid}/shares` - List active share links for the album (HasWriteAccess). Envelope `{ links: [...] }`. `password_hash` is never exposed.
- `DELETE /api/v1/shares/{slug}` - Revoke a share link (HasWriteAccess).
- `GET /api/v1/public/share/{slug}/` - Public metadata (no auth). Returns `{ has_password, expires_at, album: { title, photo_count, cover_thumb_url } }`. 404 when unknown; 410 when expired. Album payload is hidden until the recipient verifies the password.
- `POST /api/v1/public/share/{slug}/verify` - Public password check (no auth). Sets a 24h `share_<slug>` HttpOnly cookie on success. Rate-limited to 10 attempts per IP per 5 minutes (429 + `Retry-After`).
- `GET /api/v1/public/share/{slug}/photos` - Paginated public photo listing (`limit` capped at 1000, default 200). Requires the share cookie when password-protected.
- `GET /api/v1/public/share/{slug}/photos/{photo_uid}/thumb/{size}` - Public thumbnail stream (no app auth; share cookie when protected). Photo must belong to the linked album.
- `GET /api/v1/public/share/{slug}/photos/{photo_uid}/download` - Public original download with Range support.
- `GET /api/v1/labels` - List labels (native; query: `q`, `min_photos`, `sort` = `name`/`-name`/`count`/`-count`, `limit`/`count` alias, `offset`; envelope is the bare array, `description`/`notes` kept on the wire as empty strings for backwards compatibility)
- `GET /api/v1/labels/{uid}` - Get single label
- `PUT /api/v1/labels/{uid}` - Update label (`{ name?, priority?, favorite? }`; non-empty name re-slugs with collision suffix)
- `DELETE /api/v1/labels` - Batch delete labels (returns count of UIDs that actually existed; unknown UIDs are silently skipped)
- `GET /api/v1/photos` - List photos (native; archived excluded by default, filters: `album_uid`/`label_uid`/`subject_uid`/`favorite`/`private`/`archived`/`taken_from`/`taken_to`/`min_lat`+`min_lng`+`max_lat`+`max_lng`/`q`/`sort`/`limit`/`offset`; envelope `{photos, total, limit, offset}`)
- `GET /api/v1/photos/{uid}` - Get single photo (native; 404 for archived unless `?include_archived=true`)
- `PUT /api/v1/photos/{uid}` - Update photo
- `PUT /api/v1/photos/{uid}/exif` - Edit EXIF metadata (taken_at, GPS, camera/lens/exposure, title/description/notes). Writes the photo row AND an XMP sidecar next to the original (same dir + basename + `.xmp`) via `exiftool`; sidecar errors are logged but do not fail the request. Validates year ∈ [1900, 2100], lat/lng ranges, ISO > 0. `HasWriteAccess` required.
- `GET /api/v1/photos/{uid}/thumb/{size}` - Stream cached thumbnail from `<cache>/thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg` (immutable cache headers + `ETag: "sha:<hash>:<size>"`; 404 when the file is missing)
- `GET /api/v1/photos/{uid}/download` - Stream the original primary file as an attachment with `Range` support
- `GET /api/v1/photos/{uid}/faces` - Get faces in a photo with suggestions
- `POST /api/v1/photos/{uid}/faces/compute` - Compute face embeddings for a photo
- `GET /api/v1/photos/{uid}/estimate-era` - Estimate photo era from CLIP embeddings vs era centroids
- `GET /api/v1/photos/{uid}/albums` - Get album memberships for a photo
- `GET /api/v1/photos/{uid}/books` - Get book/section memberships for a photo
- `POST /api/v1/photos/similar` - Find similar photos by embedding
- `POST /api/v1/photos/similar/collection` - Find similar photos across a collection
- `POST /api/v1/photos/search-by-text` - Text-to-image similarity search (auto-translates Czech via GPT-4.1-mini)
- `POST /api/v1/photos/batch/labels` - Batch add labels to photos
- `POST /api/v1/photos/batch/edit` - Batch edit photos (favorite, private)
- `POST /api/v1/photos/batch/archive` - Archive (soft-delete) photos
- `POST /api/v1/photos/batch/restore` - Restore (un-archive) photos
- `GET /api/v1/photos/trash` - List archived photos (trash view; same filters/sort/pagination as `GET /photos`, the `archived` query param is ignored). Any authenticated role.
- `POST /api/v1/photos/batch/purge` - Hard-delete archived photos (admin only). Per UID: skip-with-error if not archived; otherwise drop the photo row (cascades phashes / markers / files / album_photos / photo_labels), the embedding row, cached face rows, on-disk originals, and every cached thumbnail size. A background daemon in `cmd/serve.go` runs the same logic hourly against photos older than `TRASH_RETENTION_DAYS` (default 30).
- `POST /api/v1/photos/duplicates` - Find near-duplicate photos via embedding similarity
- `POST /api/v1/photos/suggest-albums` - Album completion via pgvector centroid search
- `POST /api/v1/sort` - Start AI sort job
- `GET /api/v1/sort/{jobId}` - Get sort job status
- `GET /api/v1/sort/{jobId}/events` - SSE stream for job progress
- `DELETE /api/v1/sort/{jobId}` - Cancel sort job
- `POST /api/v1/upload` - Upload photos (multipart)
- `POST /api/v1/upload/job` - Start background upload job (multipart with config)
- `GET /api/v1/upload/{jobId}/events` - SSE stream for upload job progress
- `DELETE /api/v1/upload/{jobId}` - Cancel upload job
- `GET /api/v1/config` - Get available providers and version info
- `GET /api/v1/stats` - Get processing statistics
- `GET /api/v1/subjects` - List subjects (people)
- `GET /api/v1/subjects/{uid}` - Get single subject
- `PUT /api/v1/subjects/{uid}` - Update subject (rename, etc.)
- `POST /api/v1/faces/match` - Find photos matching a person's face
- `POST /api/v1/faces/apply` - Apply face match (create/assign/unassign marker)
- `POST /api/v1/faces/outliers` - Detect face outliers for a person
- `POST /api/v1/process` - Start photo processing job (embeddings + face detection)
- `GET /api/v1/process/{jobId}/events` - SSE stream for process job progress
- `DELETE /api/v1/process/{jobId}` - Cancel process job
- `POST /api/v1/process/sync-cache` - Re-derive cached face marker metadata (photo dimensions, orientation, subject linkage) from the canonical native `markers` table; useful after bulk data fixes outside the UI
- `POST /api/v1/process/build-thumbs` - Admin-only thumbnail backfill. Body: `{ concurrency?, sizes?, only_missing?, limit?, photo_uid? }`. Returns `{ job_id }`; progress streams via `/process/{jobId}/events` with `progress` (`{done,total,current_photo_uid}`) and final `summary` (`{generated,skipped,failed}`) events. Reuses the existing `ProcessJobManager` — one job at a time. Backed by `cache build-thumbs` CLI / `internal/thumb.GenerateSizes`.
- `GET /api/v1/books` - List all photo books
- `POST /api/v1/books` - Create a new book
- `GET /api/v1/books/{id}` - Get book detail with chapters, sections, and pages
- `PUT /api/v1/books/{id}` - Update book (title, description, typography settings)
- `GET /api/v1/fonts` - List available fonts for book typography
- `DELETE /api/v1/books/{id}` - Delete book (cascades to chapters, sections, pages, slots)
- `POST /api/v1/books/{id}/chapters` - Create chapter in a book
- `PUT /api/v1/books/{id}/chapters/reorder` - Reorder chapters
- `PUT /api/v1/chapters/{id}` - Update chapter (title)
- `DELETE /api/v1/chapters/{id}` - Delete chapter
- `POST /api/v1/books/{id}/sections` - Create section in a book (optional chapter_id)
- `PUT /api/v1/books/{id}/sections/reorder` - Reorder sections
- `PUT /api/v1/sections/{id}` - Update section (title, chapter_id)
- `DELETE /api/v1/sections/{id}` - Delete section
- `GET /api/v1/sections/{id}/photos` - Get photos in a section
- `POST /api/v1/sections/{id}/photos` - Add photos to a section
- `DELETE /api/v1/sections/{id}/photos` - Remove photos from a section
- `PUT /api/v1/sections/{id}/photos/{photoUid}/description` - Update section photo (description, note)
- `POST /api/v1/books/{id}/pages` - Create page in a book
- `PUT /api/v1/books/{id}/pages/reorder` - Reorder pages
- `PUT /api/v1/pages/{id}` - Update page (format, section, description, split_position)
- `DELETE /api/v1/pages/{id}` - Delete page
- `PUT /api/v1/pages/{id}/slots/{index}` - Assign photo or text to page slot (`{ photo_uid }` or `{ text_content }`)
- `PUT /api/v1/pages/{id}/slots/{index}/crop` - Update crop for a slot (`{ crop_x, crop_y, crop_scale? }`, position 0.0-1.0, scale 0.1-1.0)
- `POST /api/v1/pages/{id}/slots/swap` - Swap two slots atomically (`{ slot_a, slot_b }`)
- `DELETE /api/v1/pages/{id}/slots/{index}` - Clear page slot
- `POST /api/v1/books/{id}/sections/{sectionId}/auto-layout` - Auto-generate pages from unassigned section photos
- `GET /api/v1/books/{id}/preflight` - Validate book before PDF export (empty slots, low DPI, unplaced photos). Accepts `photo_quality=low|medium|original` to enable tier-specific warnings (e.g. `original_downgrade` for photos whose primary file is < 3840 px on the longest side).
- `GET /api/v1/books/{id}/export-pdf` - Export book as PDF synchronously (blocking ~4 min, for CLI/MCP). Accepts `photo_quality=low|medium|original` (default `medium`): `low` uses fit_720 thumbnails for previews, `medium` uses fit_3840 (current behaviour), `original` downloads the full primary file and downscales to a longest-side cap of 8000 px. HEIC/RAW primaries fall back to the fit_7680 thumbnail (pure-Go binary has no HEIC decoder).
- `POST /api/v1/books/{id}/export-pdf/job` - Start background PDF export job (UI flow, returns `{job_id}`; 409 if one is running for the same book). Accepts the same `photo_quality` query param as the sync endpoint.
- `GET /api/v1/book-export/{jobId}` - Get export job state
- `GET /api/v1/book-export/{jobId}/events` - SSE stream of progress events (phases: `fetching_metadata`, `downloading_photos`, `compiling_pass1`, `compiling_pass2`)
- `GET /api/v1/book-export/{jobId}/download` - Stream compiled PDF temp file (supports range, sets `X-Accel-Buffering: no`)
- `DELETE /api/v1/book-export/{jobId}` - Cancel export job (SIGKILLs lualatex, removes temp file)
- `GET /api/v1/pages/{id}/export-pdf` - Export single page as PDF (inline preview, requires lualatex)
- `POST /api/v1/text/check` - AI text check (spelling, grammar, diacritics, readability suggestions) via GPT-5.4-mini. Responses include a `suggestions[]` array where each item has `severity` (`major`/`minor`) and `message`. `CheckAndSave` uses a 3-tier cache: in-memory → DB (by `(source_type, source_id, field)` + `content_hash`) → OpenAI, so unchanged texts never burn a second OpenAI call after a server restart.
- `POST /api/v1/text/check-and-save` - AI text check with database persistence
- `POST /api/v1/text/rewrite` - AI text rewrite (length adjustment) via GPT-4.1-mini
- `POST /api/v1/text/consistency` - AI style consistency check across all book texts via GPT-4.1-mini
- `GET /api/v1/books/{id}/text-check-status` - Get text check status for all book texts
- `GET /api/v1/text-versions` - List text version history
- `POST /api/v1/text-versions/{id}/restore` - Restore a previous text version
- `GET /api/v1/me` - Currently authenticated user
- `POST /api/v1/me/password` - Change own password (current + new)
- `GET /api/v1/users` - List users (admin only)
- `POST /api/v1/users` - Create user (admin only)
- `GET /api/v1/users/{uid}` - Get user (admin only)
- `PUT /api/v1/users/{uid}` - Update user (admin only)
- `POST /api/v1/users/{uid}/password` - Reset another user's password (admin only)
- `POST /api/v1/users/{uid}/disable` - Disable/enable a user (admin only)
- `DELETE /api/v1/users/{uid}` - Delete a user (admin only; the last admin cannot be deleted)

**Frontend Structure:**
```
web/src/
├── api/
│   └── client.ts              # Typed API client
├── components/                # Shared UI components
│   ├── AccentCard.tsx         # Accent-colored card
│   ├── Alert.tsx              # Alert/notification component
│   ├── BulkActionBar.tsx      # Bulk action panel for photo selection
│   ├── Button.tsx
│   ├── Card.tsx
│   ├── Combobox.tsx           # Autocomplete combobox (label/album filters)
│   ├── ConfirmDialog.tsx      # Reusable confirmation dialog
│   ├── ErrorBoundary.tsx      # Error catching wrapper
│   ├── FormCheckbox.tsx       # Styled checkbox with label
│   ├── FormInput.tsx          # Styled text/number input with label
│   ├── FormSelect.tsx         # Styled select dropdown with label
│   ├── LanguageSwitcher.tsx   # Czech/English language toggle
│   ├── LazyImage.tsx
│   ├── Layout.tsx
│   ├── LoadingState.tsx       # Unified loading/error/empty states
│   ├── PageHeader.tsx         # Page header with title/actions
│   ├── PhotoCard.tsx
│   ├── PhotoGrid.tsx          # Supports optional selection mode
│   ├── PhotoWithBBox.tsx
│   └── StatsGrid.tsx          # Stats display grid (configurable columns/colors)
├── constants/
│   ├── actions.ts             # Face action styling (i18n label keys, colors)
│   ├── bookTypography.ts      # Typography CSS defaults, font registry, CSS variable helpers
│   ├── index.ts               # Magic numbers and defaults
│   └── pageConfig.ts          # Book page format configuration
├── hooks/                     # Global hooks
│   ├── useAuth.tsx
│   ├── useBookKeyboardNav.ts  # Book editor keyboard nav (W/S/E/D)
│   ├── useFaceApproval.ts     # Face approval logic (single + batch)
│   ├── usePhotoSelection.ts   # Shared photo selection + bulk actions
│   ├── useSSE.ts              # Server-Sent Events
│   └── useSubjectsAndConfig.ts # Shared data loading
├── i18n/                      # Internationalization (Czech + English)
│   ├── index.ts
│   └── locales/{cs,en}/       # common.json, forms.json, pages.json
├── utils/
│   ├── clipboard.ts           # Clipboard copy utility
│   ├── fontLoader.ts          # Google Fonts CSS loader (deduplicates, display=swap)
│   ├── markdown.ts            # Markdown-to-HTML renderer (marked.js + DOMPurify)
│   └── pageFormats.ts         # Book page format helpers
├── pages/                     # Page components
│   ├── Albums.tsx             # Album listing
│   ├── Dashboard.tsx          # Home dashboard
│   ├── Expand.tsx             # Album expansion/suggestions
│   ├── Labels.tsx             # Label listing
│   ├── LabelDetail.tsx        # Single label detail
│   ├── Login.tsx              # Login page
│   ├── Outliers.tsx           # Face outlier detection
│   ├── Process.tsx            # Embedding/face processing
│   ├── SimilarPhotos.tsx      # Similar photo results
│   ├── SubjectDetail.tsx      # Single person/subject detail
│   ├── TextSearch.tsx         # Text-to-image search
│   ├── Analyze/               # AI analysis (hooks/useSortJob.ts)
│   ├── Faces/                 # Face matching (hooks/useFaceSearch.ts)
│   ├── Photos/                # Photo browsing (hooks/usePhotosFilters.ts, usePhotosPagination.ts)
│   ├── PhotoDetail/           # Photo detail (hooks/usePhotoData.ts, useFacesData.ts, useFaceAssignment.ts, usePhotoNavigation.ts)
│   │   ├── EraEstimate.tsx, AlbumMembership.tsx, BookMembership.tsx, AddToBookDropdown.tsx
│   │   ├── FacesList.tsx, FaceAssignmentPanel.tsx, EmbeddingsStatus.tsx
│   │   └── PhotoDisplay.tsx
│   ├── Recognition/           # Bulk face recognition (hooks/useScanAll.ts)
│   ├── Duplicates/            # Near-duplicate detection
│   ├── Compare/               # Side-by-side comparison (hooks/useCompareState.ts)
│   ├── Books/                 # Photo books list
│   ├── BookEditor/            # Book editor (sections, pages, preview, typography, texts, duplicates)
│   │   ├── hooks/useBookData.ts, hooks/useUndoRedo.ts, hooks/useBookExportJob.ts
│   │   ├── BookStatsPanel.tsx, KeyboardShortcutsHelp.tsx
│   │   ├── SectionsTab.tsx, SectionSidebar.tsx, SectionPhotoPool.tsx
│   │   ├── PagesTab.tsx, PageSidebar.tsx, PageMinimap.tsx, PageTemplate.tsx, PageSlot.tsx
│   │   ├── UnassignedPool.tsx, PreviewTab.tsx, PreviewModal.tsx
│   │   ├── TypographyTab.tsx, TextsTab.tsx, DuplicatesTab.tsx
│   │   ├── ExportProgressModal.tsx, PhotoBrowserModal.tsx, PhotoDescriptionDialog.tsx
│   │   └── PhotoActionOverlay.tsx, PhotoInfoOverlay.tsx
│   ├── Slideshow/             # Photo slideshow (hooks/useSlideshow.ts, useSlideshowPhotos.ts)
│   ├── SuggestAlbums/         # Album completion
│   └── Upload/                # Photo upload (hooks/useUploadJob.ts, DropZone.tsx)
└── types/
    ├── events.ts              # Typed SSE events (discriminated unions)
    └── index.ts               # API response types
```

**Shared Hooks:**
- `useBookKeyboardNav` - Book editor keyboard nav (W/S/E/D). Used by BookEditor.
- `useFaceApproval` - Single and batch face approval. Used by Faces, Recognition, PhotoDetail.
- `usePhotoSelection` - Photo selection + bulk actions (add to album, label, favorite, remove). Used by Photos, SimilarPhotos, Expand, Duplicates.
- `useSubjectsAndConfig` - Loads subjects and config in parallel. Used by Faces, Recognition, Outliers.
- `useSSE` - Server-Sent Events for real-time job progress. Used by Analyze and Process.

**Handler Structure:**
```
internal/web/handlers/
├── auth.go, albums.go, labels.go, photos.go   # Core CRUD
├── sort.go, upload.go, upload_job.go, process.go # Jobs
├── config.go, stats.go, sse.go, common.go      # Utilities
├── subjects.go                                 # Subject CRUD
├── faces.go                                    # FacesHandler struct
├── face_match.go, face_apply.go                # Face matching and applying
├── face_outliers.go, face_photos.go            # Outlier detection, photo faces
├── face_helpers.go                             # Shared face helpers
├── books.go                                    # BooksHandler: books, chapters, sections, pages, slots, fonts
├── book_export_job.go                          # BookExportJob type, manager with TTL sweeper, 5 job-flow handlers on BooksHandler, background runner that translates latex progress into SSE events
├── text.go                                     # TextHandler: AI text check, rewrite, consistency, check-and-save
├── text_versions.go                            # TextVersionsHandler: text version history and restore
└── jobs.go                                     # Sort job status
```

**Photo Book Database:**

Tables: `photo_books` (with typography: `body_font`, `heading_font`, `body_font_size`, `body_line_height`, `h1_font_size`, `h2_font_size`, `caption_opacity`, `caption_font_size`, `heading_color_bleed` — migrations 021-023; plus `body_text_pad_mm` from migration 029 for inner padding of body text on the photo-adjacent side of a text slot in mixed layouts), `book_chapters` (with optional `color` for per-chapter theme, migration 020), `book_sections` (with optional `chapter_id`), `section_photos`, `book_pages`, `page_slots` (migration 008, extended by 009-013, 015, 026 plus crop/split/chapter features). Hierarchy: Book > Chapters (optional) > Sections > Pages > Slots. Slots hold either `photo_uid`, `text_content`, or `is_captions_slot` (mutually exclusive via CHECK constraint; at most one captions slot per page, enforced by partial unique index from migration 026) with `crop_x`/`crop_y` for crop positioning (0.0-1.0, default 0.5) and `crop_scale` for zoom level (0.1-1.0, default 1.0). Pages have a `style` field (`modern`/`archival`, migration 013), `split_position` for adjustable column splits in `2l_1p`/`1p_2l` formats (0.2-0.8, default 0.5), and `hide_page_number` (BOOLEAN, default false, migration 025) which suppresses folio rendering on a single page without breaking pagination of the rest. A captions slot routes the page's `FooterCaption` list into its position in the slot grid and suppresses the bottom captions strip — see `docs/photo-book.md` for details. Pages with no photos and no text in any slot are preserved end-to-end (rendered as blank pages with their folio) so users can insert deliberate blanks; the `latex.buildSection` rendering loop has no skip filter.

Page formats: `4_landscape` (4 slots), `2l_1p` (3 slots), `1p_2l` (3 slots), `2_portrait` (2 slots), `1_fullscreen` (1 slot), `1_fullbleed` (1 slot). Layout uses a 12-column grid with 3 fixed zones (header 4mm / canvas 172mm / footer 8mm) and asymmetric margins (inside 20mm / outside 12mm). Mixed formats support adjustable split position via `split_position`. `1_fullbleed` is special: the photo bypasses the safe canvas and covers the full A4+3mm bleed area (303×216 mm), and folio + footer captions are automatically suppressed for the page (manual-only — not produced by auto-layout).

**Text Slot Markdown:** Text slots support GFM markdown: headings (`#`/`##`), bold, italic, lists, blockquotes, alignment macros (`->text<-` for center, `->text->` for right-align), and tables (GFM pipe syntax). Tables support optional column width percentages in the separator row (e.g., `|--- 60% ---|--- 40% ---|`). Frontend renders via `marked.js` + DOMPurify with `<colgroup>` width injection; PDF uses `tabularx` with `\hsize`-scaled `X` columns. Text type auto-detection: T1 (explanation), T2 (fact box/list), T3 (oral history/blockquote).

The frontend is embedded in the Go binary at compile time via `go:embed`. Run `make build` to build both frontend and backend into a single binary.

**Common Pitfall - Bounding Box Positioning:**
When rendering bounding boxes as absolute-positioned overlays on images, the parent container MUST have `position: relative`. Otherwise, percentage-based coordinates will be relative to the wrong ancestor.

**Common Pitfall - Subject/Album Thumbnail Hashes:**
The `Thumb` field on `Subject` and `Album` structs is a **file hash**, not a photo UID. It cannot be used with `getThumbnailUrl(uid, size)` which expects a photo UID. Use a fallback icon instead.

### Photo Faces API

The `GET /api/v1/photos/:uid/faces` endpoint combines faces from the embeddings database (InsightFace) and rows in the native `markers` table, matched via IoU (threshold >= 0.1) in display coordinate space.

**Minimum face size:** `GetPhotoFaces` does NOT filter by face size (for manual inspection). `Match` endpoint applies minimum size filtering (`MinFaceWidthPx = 35`, `MinFaceWidthRel = 0.01` from `constants.go`).

**Unmatched markers:** Appended with negative `face_index` (-1, -2, ...), `bbox_rel` from marker coordinates, no suggestions.

### Face Outlier Detection

Detects wrongly assigned faces by computing the centroid of a person's face embeddings and ranking by cosine distance. Faces with `missing_embeddings` (a marker exists but no InsightFace embedding is stored) have `face_index: -1` and `dist_from_centroid: -1`.

**Coordinate handling:** Both markers and InsightFace embeddings use display-space coordinates. For EXIF orientations 5-8 (90° rotations), raw file dimensions must be swapped for display. The `convertPixelBBoxToDisplayRelative` function handles this.

**Unassigning faces:** `POST /api/v1/faces/apply` with `action: "unassign_person"` calls `ClearMarkerSubject`.

### Recognition Page

Scans all known people for high-confidence face matches. Iterates subjects with `photo_count > 0`, calls `matchFaces` with concurrency 3, filters to actionable matches only (`create_marker` or `assign_person`). Results stream incrementally per person. Confidence maps to distance: `distanceThreshold = 1 - confidence / 100`.

### Native API Endpoint Refresher

For the full endpoint catalogue see [`docs/API.md`](docs/API.md); the
self-management surface lives at `/api/v1/me/*` (any role) and
`/api/v1/users/*` (admin only). Trash is at `/api/v1/photos/trash`,
`/api/v1/photos/batch/restore`, and `/api/v1/photos/batch/purge` (admin
only). EXIF edits go through `PUT /api/v1/photos/{uid}/exif`. Public
album share links are minted at `POST /api/v1/albums/{uid}/share`
(HasWriteAccess) and consumed anonymously under
`/api/v1/public/share/{slug}/*` (no session cookie). The public side
sets a per-share `share_<slug>` HttpOnly cookie after `POST /verify`
succeeds; `verify` is rate-limited to 10 attempts/IP/5min and returns
429 + `Retry-After` once exceeded.

## Documentation Requirements

**IMPORTANT:** Keep documentation updated with every code change.

When adding or modifying features, update the relevant documentation:

- **`docs/architecture.md`** - Update when changing system design, package structure, or data flow
- **`docs/cli-reference.md`** - Update when adding/changing CLI commands or flags
- **`docs/web-ui.md`** - Update when adding/changing Web UI pages or features
- **`docs/markers.md`** - Update when changing marker/face matching logic or coordinate handling
- **`docs/era-estimation.md`** - Update when changing era estimation logic, centroids, or UI
- **`docs/photo-book.md`** - Update when changing photo book feature (formats, schema, UI)
- **`docs/API.md`** - Update when changing REST API endpoints
- **`docs/testing-environment.md`** - Update when changing dev/test setup
- **`docs/backup.md`** - Operator runbook for backing up and restoring a photo-sorter install
- **`.goreleaser.yaml` + `deb/`** - The release surface: goreleaser config, systemd unit, sample env conffile, and maintainer scripts that produce the published `.deb` package. Update both when adding new runtime dependencies (e.g. a new system binary the upload pipeline shells out to needs a matching entry in `nfpms.dependencies`) or new env vars (add a commented-out line to `deb/photo-sorter.env`).
- **`README.md`** - Update for major feature additions or architectural changes

Documentation files:
```
docs/
├── API.md                       # REST API documentation
├── architecture.md              # System design, package structure, and data flow
├── backup.md                    # Operator runbook for backing up and restoring a photo-sorter install
├── cli-reference.md             # Complete CLI command reference
├── era-estimation.md            # Era estimation: centroids, API, and UI
├── similarity-search.md         # pgvector cosine search: indexes, ef_search, maintenance
├── markers.md                   # Native markers table and face-to-marker matching
├── migration-from-photoprism.md # One-shot PhotoPrism → photo-sorter runbook
├── photo-book.md                # Photo book planning tool
├── testing-environment.md       # Dev/test environment setup
└── web-ui.md                    # Web UI features and API endpoints
```
