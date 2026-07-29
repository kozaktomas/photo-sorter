# Testing Environment Documentation

This document describes the testing environment configured for development and automated testing.

## dev.sh

The `./dev.sh` script is the canonical local entrypoint: it stops any
running `photo-sorter serve`, runs `npm install` if `web/node_modules`
is older than `package-lock.json`, builds the frontend with `tsc -b &&
vite build` when `web/src/**`, `web/public/**`, `web/index.html`,
`vite.config.ts`, `tsconfig*.json`, or `package.json` are newer than
`internal/web/static/dist/index.html`, builds the Go binary when any
`.go` file (or `go.mod` / `go.sum`) is newer than the `photo-sorter`
binary, sources `.env.dev`, and starts `photo-sorter serve` on port
8085 (override with `PORT=…`) in the background. Logs land in
`./photo-sorter.log` and the script waits for `/api/v1/health` to
become green before returning.

Pass `--force` to bypass the smart-caching checks and rebuild every
stage. The script also warns if the canonical book-fonts sentinel
(`/usr/local/share/fonts/photo-sorter/truetype/lato/Lato-Regular.ttf`)
is missing — PDF export will fail until `make install-fonts` has run.

## Docker Compose Setup

The testing environment consists of a single managed container defined in
`docker-compose.yml` — PostgreSQL with pgvector. The PhotoPrism + MariaDB
test services were retired once photo-sorter switched to its native
Postgres-backed repositories and storage layer.

### PostgreSQL with pgvector

```yaml
pgvector-test:
  image: pgvector/pgvector:pg17
  container_name: pgvector
```

**Credentials:**
- Host: `pgvector` (container name) / `localhost:5433` (from host)
- User: `postgres`
- Password: `photoprism`
- Database: `postgres` (default)

**Extensions:**
- `vector` (pgvector 0.8.1) — vector similarity search with HNSW indexes
- `unaccent` — diacritic-insensitive text comparison

### External services (not in compose)

- **Embeddings (CLIP + faces):** runs externally over Tailscale. Configure
  via `EMBEDDING_URL` in `.env.dev`.
- **Originals + cache:** local filesystem under `STORAGE_ORIGINALS_PATH`
  (default `./data/originals`) and `STORAGE_CACHE_PATH` (default
  `./data/cache`). The originals tree uses the layout
  `YYYY/MM/<filename>` (same shape PhotoPrism uses so a migrated tree can
  be reused in place); the cache stores thumbnails as
  `thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg`.

### Bootstrap admin

The first admin user is created automatically from
`BOOTSTRAP_ADMIN_USERNAME` + `BOOTSTRAP_ADMIN_PASSWORD` when the `users`
table is empty. Set both in `.env.dev`:

```env
BOOTSTRAP_ADMIN_USERNAME=admin
BOOTSTRAP_ADMIN_PASSWORD=dev-password
```

If either variable is missing on a fresh install, the server logs a WARN
and starts anyway — you'll have to create the first user manually before
you can log in. Once any user exists the bootstrap path is a no-op, so
the variables can safely stay in `.env.dev` forever.

### Trash + duplicate detection knobs

| Variable | Default | What to override it for |
|----------|---------|-------------------------|
| `TRASH_RETENTION_DAYS` | 30 | Drop to `1` while testing the hourly auto-purge daemon end-to-end. |
| `DUPLICATE_CHECK_ENABLED` | `true` | Set to `false` to bypass the pHash + embedding scan when uploading manufactured fixtures. |
| `DUPLICATE_PHASH_MAX_DIFF` | 8 | Larger → looser matching (more reported near-duplicates). |
| `DUPLICATE_EMBEDDING_MAX_DIST` | 0.05 | Larger → looser matching. |

## Access Methods

### PostgreSQL with pgvector

Connect using psql:
```bash
PGPASSWORD=photoprism psql -h localhost -p 5433 -U postgres -d postgres
```

Test vector operations:
```sql
-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- Create table with vector column
CREATE TABLE test_embeddings (
    id SERIAL PRIMARY KEY,
    embedding VECTOR(512)
);

-- Insert vectors
INSERT INTO test_embeddings (embedding) VALUES ('[1,2,3,...,512]'::vector);

-- Cosine similarity search
SELECT id, embedding <=> '[1,2,3,...,512]'::vector AS distance
FROM test_embeddings
ORDER BY embedding <=> '[1,2,3,...,512]'::vector
LIMIT 10;
```

Create HNSW index for fast similarity search:
```sql
CREATE INDEX idx_embeddings_hnsw ON test_embeddings
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 200);
```

Test unaccent extension (for diacritic-insensitive name matching):
```sql
CREATE EXTENSION IF NOT EXISTS unaccent;
SELECT unaccent('Příliš žluťoučký kůň');
-- Returns: Prilis zlutoucky kun
```

**Key tables (photo-sorter native schema):**

| Table | Description |
|-------|-------------|
| `photos` | Photo metadata (the canonical native photo row) |
| `albums`, `album_photos` | Albums and membership |
| `labels`, `photo_labels` | Labels and tagging |
| `subjects`, `markers` | People and face markers |
| `embeddings` | 768-dim CLIP image embeddings |
| `faces` | 512-dim face embeddings + cached marker data |
| `faces_processed` | Tracks which photos have been processed |
| `sessions` | Web sessions persisted across restarts |
| `users` | Native user accounts (admin/editor/viewer) |
| `photo_books`, `book_chapters`, `book_sections`, `book_pages`, `page_slots`, `section_photos` | Photo book hierarchy |
| `text_versions`, `text_check_results` | Text version history + AI text-check cache |
| `schema_migrations` | Applied database migrations |

## Volume Mounts

Data is persisted in local volumes:
```
./volumes/pgvector_data              → /var/lib/postgresql/data
```

## Network Configuration

The pgvector container is exposed on host port `5433`. Other dev processes
(the photo-sorter binary run by `./dev.sh`, the external embeddings service)
connect through the host network.

## Starting the Environment

```bash
# Start pgvector
docker compose up -d

# Check status
docker compose ps

# View logs
docker compose logs pgvector-test

# Stop services
docker compose down
```

## External Tools

The upload pipeline shells out to a handful of system binaries. The Docker
image installs them automatically; for dev environments run outside Docker
you need them on `PATH`:

| Binary | Package | Purpose |
|--------|---------|---------|
| `exiftool` | `exiftool` (Debian / Alpine) | EXIF reader fallback + XMP sidecar writer |
| `heif-convert` | `libheif-tools` / `libheif-examples` | HEIC/HEIF decoder |
| `dcraw` | `libraw-tools` (Alpine) / `dcraw` (Debian) | RAW decoder; on Alpine, `scripts/dcraw-shim.sh` wraps `dcraw_emu` |

The serve command logs a `WARN` line for each missing binary on startup so
deployments fail loud, not silent.

### PostgreSQL client tools

`backup` / `db-export` shell out to `pg_dump` and `db-import` to
`pg_restore` (`apt: postgresql-client-17`). `TestPgDump_subprocess` in
`cmd/backup_test.go` exercises the real `pg_dump` binary against a
throwaway `postgres:17-alpine` container, and silently skips itself when
`pg_dump` or `docker` is missing from `PATH`.

**The client must be version 17 or newer.** `pg_dump` refuses to dump a
server newer than itself, so a version 16 client against the Postgres 17
test container (and against production) aborts with `server version
mismatch`. Ubuntu's stock client can lag behind — install
`postgresql-client-17` from the [PGDG apt repository](https://apt.postgresql.org/)
and make sure `/usr/lib/postgresql/17/bin` comes before `/usr/bin` on
`PATH`, since `/usr/bin/pg_dump` is `pg_wrapper` and may resolve to an
older versioned binary. The `test` job in `.github/workflows/test.yml`
does exactly this and asserts the resolved major version, so CI fails
loudly rather than skipping the test.

## Book Typography Fonts

PDF export (the book exporter at `internal/latex/`) needs the book fonts
installed on the host. Production reads them from the Docker image's
`/usr/share/fonts`; for dev environments outside Docker, install them
once after cloning:

```bash
make install-fonts
```

The target shells into `scripts/install-fonts.sh` — the same script the
Docker build runs — under `sudo` and writes the 24 free book fonts to
`/usr/local/share/fonts/photo-sorter`. The script is idempotent (skips
already-installed files) and refreshes the fontconfig cache for that
directory on success.

The system path matters. `compileLatex` in `internal/latex/latex.go`
overrides `HOME` to a fresh temp dir before spawning `lualatex` (so
`luaotfload` writes its cache there), which hides any user-local font
directory from fontconfig. Installing to `~/.local/share/fonts/...`
would silently break PDF export.

Bookman Old Style is proprietary (Microsoft) and is NOT installed by
the script. It is registered in `internal/latex/fonts.go` but operators
who need it must drop the licensed `BOOKOS*.TTF` files into
`/usr/local/share/fonts/photo-sorter/truetype/bookman-old-style/` and
rerun `fc-cache -f` + `luaotfload-tool --update --force`.

`dev.sh` warns when the canonical sentinel (`Lato-Regular.ttf`) is
missing so a fresh checkout cannot quietly land a broken PDF pipeline.

## Go Integration Tests

The project includes integration tests for the PostgreSQL/pgvector backend
using testcontainers-go.

### Running Integration Tests

```bash
# Run all tests (unit + integration)
go test -tags=integration ./...

# Run only database integration tests
go test -tags=integration -v ./internal/database/postgres/

# Run a specific test
go test -tags=integration -v ./internal/database/postgres/ -run TestEmbeddingRepository
```

### Test Structure

Integration tests use the `//go:build integration` build tag and spin up a
temporary PostgreSQL container with pgvector:

```go
//go:build integration

func TestEmbeddingRepository(t *testing.T) {
    pool, cleanup := setupTestContainer(t)
    if pool == nil {
        return // Skip if Docker unavailable
    }
    defer cleanup()

    repo := NewEmbeddingRepository(pool)
    // Test repository methods...
}
```

The test container:
- Image: `pgvector/pgvector:pg16`
- Credentials: `test` / `test` / `testdb`
- Automatically runs migrations on startup
- Cleans up after test completion

### Manual Testing with docker-compose

For manual testing against the persistent pgvector container:

```bash
# Set DATABASE_URL for the app (from the host)
export DATABASE_URL="postgres://postgres:photoprism@localhost:5433/postgres?sslmode=disable"

# Run the app
go run . serve
```

## Test Commands

The Makefile is the canonical source. The Go side uses the race
detector with explicit paths (so traversal never hits the root-owned
`./volumes` tree).

```bash
# Run the whole Go test suite with -race
make test

# Same, with verbose output
make test-v

# Run a single package or test
go test -v ./internal/photoprism/
go test -v ./internal/photoprism/ -run TestGetAlbum
```

## Quality Gate

`make check` is the full quality gate the CI runs:

```bash
make check    # fmt + vet + lint + test
```

Sub-targets are also available individually:

| Target | What it does |
|--------|--------------|
| `make fmt` | `goimports -w . && go fmt ./...` |
| `make vet` | `go vet . ./cmd/... ./internal/...` |
| `make lint` | `golangci-lint run . ./cmd/... ./internal/...` |
| `make lint-fix` | same, with `--fix` |
| `make test` | `go test -race . ./cmd/... ./internal/...` |

### Pre-commit Hook

A pre-commit hook is wired up at the repo level and is scoped to the
files actually being committed:

- **Go changes:** `make lint` must pass.
- **Frontend changes:** `npx tsc --noEmit` and `npm run lint` (both in
  `web/`) must pass.

The hook short-circuits when none of the staged files touch the
relevant language, so a docs-only commit never spends time on the Go or
JS pipelines.
