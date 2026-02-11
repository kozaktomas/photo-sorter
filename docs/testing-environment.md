# Testing Environment Documentation

This document describes the testing environment configured for development and automated testing.

## Docker Compose Setup

The testing environment consists of three containers defined in `docker-compose.yml`:

### PhotoPrism Test Instance

```yaml
photoprism-test:
  image: photoprism/photoprism:latest
  container_name: photoprism-test
  ports:
    - "2342:2342"
```

**Credentials:**
- Username: `admin`
- Password: `photoprism`

**Configuration:**
- Auth mode: password
- Site URL: http://localhost:2342/
- Database: MariaDB (mysql driver)
- Face detection: enabled
- Classification: enabled

### MariaDB Database

```yaml
mariadb-test:
  image: mariadb:11
  container_name: mariadb-test
```

**Credentials:**
- Database: `photoprism`
- User: `photoprism`
- Password: `photoprism`
- Root password: `photoprism`

### PostgreSQL with pgvector

```yaml
pgvector-test:
  image: pgvector/pgvector:pg17
  container_name: pgvector
```

**Credentials:**
- Host: `pgvector` (container name)
- Port: `5432`
- User: `postgres`
- Password: `photoprism`
- Database: `postgres` (default)

**Extensions:**
- `vector` (pgvector 0.8.1) - Vector similarity search with HNSW indexes
- `unaccent` - Diacritic-insensitive text comparison

## Access Methods

### 1. PhotoPrism REST API

Access from within Docker network:
```bash
# Check status
curl http://photoprism-test:2342/api/v1/status

# Authenticate
curl -X POST http://photoprism-test:2342/api/v1/session \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"photoprism"}'

# Use token for subsequent requests
curl http://photoprism-test:2342/api/v1/albums \
  -H "X-Session-ID: <access_token>"
```

Alternative hosts:
- `photoprism-test:2342` (container name, preferred)
- `host.docker.internal:2342` (host network)
- `localhost:2342` (from host machine only)

### 2. MariaDB Database

Connect using Python (pymysql):
```python
import pymysql

conn = pymysql.connect(
    host='mariadb-test',
    user='photoprism',
    password='photoprism',
    database='photoprism'
)
cursor = conn.cursor()
cursor.execute('SHOW TABLES')
```

**Key Tables:**
| Table | Description |
|-------|-------------|
| `photos` | Photo metadata |
| `albums` | Album definitions |
| `files` | File information |
| `labels` | Labels/tags |
| `subjects` | People/subjects |
| `markers` | Face markers |
| `auth_sessions` | Active sessions |
| `auth_users` | User accounts |

### 3. PostgreSQL with pgvector

Connect using psql:
```bash
PGPASSWORD=photoprism psql -h pgvector -U postgres -d postgres
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

**Key Tables (Photo Sorter):**
| Table | Description |
|-------|-------------|
| `embeddings` | 768-dim CLIP image embeddings |
| `faces` | 512-dim face embeddings with cached PhotoPrism data |
| `faces_processed` | Tracks which photos have been processed |
| `schema_migrations` | Applied database migrations |

### 4. Playwright Browser Automation

Playwright can automate the PhotoPrism web UI:

```javascript
const { chromium } = require('playwright');

const browser = await chromium.launch({ headless: true });
const page = await browser.newPage();

// Navigate to PhotoPrism
await page.goto('http://photoprism-test:2342');

// Login
await page.locator('input[autocomplete="username"]').fill('admin');
await page.locator('input[autocomplete="current-password"]').fill('photoprism');
await page.locator('input[autocomplete="current-password"]').press('Enter');

// Wait for navigation
await page.waitForURL('**/library/browse');

// Take screenshot
await page.screenshot({ path: 'screenshot.png' });

await browser.close();
```

**Important selectors:**
- Username: `input[autocomplete="username"]`
- Password: `input[autocomplete="current-password"]`
- Submit: Press Enter on password field (button may be initially disabled)

**Playwright setup notes:**
- Chromium browser needs to be installed: `npx playwright install chromium`
- Browser version must match playwright library version
- If version mismatch, create symlinks in `~/.cache/ms-playwright/`

## Volume Mounts

Data is persisted in local volumes:
```
./volumes/photoprism-test-originals  → /photoprism/originals
./volumes/photoprism-test-storage    → /photoprism/storage
./volumes/mariadb-test-data          → /var/lib/mysql
./volumes/pgvector_data              → /var/lib/postgresql/data
```

## Network Configuration

All containers share the same Docker network, allowing:
- Container-to-container communication via container names
- Host access via `host.docker.internal`

## Starting the Environment

```bash
# Start all services
docker-compose up -d

# Check status
docker-compose ps

# View logs
docker-compose logs photoprism-test

# Stop services
docker-compose down
```

## Testing Use Cases

### API Testing
Use the REST API for:
- Creating/listing albums
- Uploading photos
- Managing labels
- Face detection results

### Database Testing
Direct database access for:
- Verifying data persistence
- Checking face embeddings
- Debugging marker assignments

### E2E Testing
Playwright for:
- UI workflow testing
- Visual regression testing
- Integration tests that require browser interaction

## Go Integration Tests

The project includes integration tests for the PostgreSQL/pgvector backend using testcontainers-go.

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

Integration tests use the `//go:build integration` build tag and spin up a temporary PostgreSQL container with pgvector:

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

### What's Tested

| Test | Description |
|------|-------------|
| `TestEmbeddingRepository` | Save, Get, Has, Count, FindSimilar operations for 768-dim embeddings |
| `TestFaceRepository` | SaveFaces, GetFaces, HasFaces, MarkProcessed, UpdateMarker, FindSimilar for 512-dim face embeddings |
| `TestMigrations` | Verifies the first 5 migrations are applied correctly (11 total migrations exist) |

### Manual Testing with docker-compose

For manual testing against the persistent pgvector container:

```bash
# Set DATABASE_URL for the app
export DATABASE_URL="postgres://postgres:photoprism@pgvector:5432/postgres?sslmode=disable"

# Run the app
go run . serve
```
