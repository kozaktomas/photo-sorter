# Testing Environment Documentation

This document describes the testing environment configured for development and automated testing.

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

The following directories are leftover from the retired PhotoPrism + MariaDB
test services and are intentionally preserved until the operator confirms the
native migration is good and backups are restorable:
```
./volumes/photoprism-test-originals
./volumes/photoprism-test-storage
./volumes/mariadb-test-data
```

`docker compose down` does not delete them (bind mounts are never managed by
compose). Remove manually after backup verification.

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
