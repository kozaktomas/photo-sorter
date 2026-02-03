# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Denied Files

Do NOT read or access the following files:
- `.envrc` - Contains sensitive environment variables

## Build and Test Commands

```bash
# Build everything (frontend + Go binary)
make build

# Build only Go binary (without frontend)
make build-go

# Build only frontend
make build-web

# Run tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run a single test
go test -v ./internal/photoprism/ -run TestGetAlbum

# Run the CLI
go run . <command>

# Start the web server
go run . serve
```

## Development Environment

**IMPORTANT:** After every code change, run the dev script to rebuild and restart the server:

```bash
./dev.sh
```

This script:
1. Stops any running photo-sorter process
2. Builds the frontend (npm install + build)
3. Builds the Go binary
4. Starts the server on port 8085 using test services (PhotoPrism + pgvector)

To check server logs:
```bash
tail -f /app/photo-sorter.log
```

The dev environment uses:
- PhotoPrism: `http://photoprism-test:2342` (admin/photoprism)
- PostgreSQL: `pgvector:5432` (postgres/photoprism)
- Embeddings: configured in `.env.dev`

## Direct PhotoPrism API Auth (for Playwright/curl)

When testing the PhotoPrism API directly (not through photo-sorter), authentication works as follows:

1. **Login:** `POST http://photoprism-test:2342/api/v1/session` with body `{"username":"admin","password":"photoprism"}`
2. **Session ID:** The response JSON contains an `id` field (same value as `access_token`) — use either as the session token. Do NOT use the `session_id` field (it's a different value and won't work).
3. **Subsequent requests:** Pass the session ID via the `X-Session-ID` header

```bash
# Login and extract session ID
TOKEN=$(curl -s -X POST http://photoprism-test:2342/api/v1/session \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"photoprism"}' | jq -r '.id')

# Use the token
curl -s -H "X-Session-ID: $TOKEN" "http://photoprism-test:2342/api/v1/photos?count=10"
```

**Photo-sorter's own API** uses cookie-based auth instead:
```bash
curl -c cookies.txt -X POST http://localhost:8085/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"photoprism"}'
curl -b cookies.txt "http://localhost:8085/api/v1/albums"
```

## Architecture

This is a CLI tool that sorts photos in PhotoPrism using AI providers. Built with Cobra for CLI and Viper for configuration.

### Core Components

- **cmd/** - Cobra commands (albums, labels, count, move, sort, upload, photo, cache, version, serve)
- **internal/photoprism/** - PhotoPrism REST API client (split by domain: albums, photos, labels, markers, subjects, upload)
- **internal/ai/** - AI provider interface with OpenAI, Gemini, Ollama, and llama.cpp implementations
- **internal/sorter/** - Orchestrates photo fetching → AI analysis → label application
- **internal/config/** - Environment-based configuration loader
- **internal/fingerprint/** - Perceptual hash computation (pHash, dHash), image/face embeddings client
- **internal/database/** - PostgreSQL storage with pgvector for embeddings and faces data
- **internal/facematch/** - Face matching utilities (IoU computation, bbox conversion, name normalization, marker matching)
- **internal/constants/** - Shared constants (page sizes, thresholds, concurrency, upload limits)
- **internal/web/** - Web server with Chi router, REST API handlers, and SSE for real-time updates
- **web/** - React + TypeScript + TailwindCSS frontend (built with Vite)

### PhotoPrism Package Structure

The PhotoPrism client is split by domain for maintainability:

```
internal/photoprism/
├── photoprism.go      # Core client: struct, constructors, auth, logout
├── http.go            # Generic HTTP helpers: doGetJSON, doPostJSON, doPutJSON, doDeleteJSON
├── types.go           # All type definitions (Album, Photo, Label, Marker, Subject, etc.)
├── albums.go          # Album operations: GetAlbum, GetAlbums, CreateAlbum, AddPhotosToAlbum, etc.
├── photos.go          # Photo operations: GetPhotos, EditPhoto, GetPhotoDetails, GetPhotoDownload, etc.
├── labels.go          # Label operations: GetLabels, UpdateLabel, DeleteLabels, AddPhotoLabel, etc.
├── markers.go         # Marker operations: GetPhotoMarkers, CreateMarker, UpdateMarker, etc.
├── subjects.go        # Subject operations: GetSubject, UpdateSubject, GetSubjects
├── faces.go           # Face operations: GetFaces
└── upload.go          # Upload operations: UploadFile, UploadFiles, ProcessUpload
```

**HTTP Helpers:** The `http.go` file provides generic helpers that reduce boilerplate:
```go
// Generic GET returning typed response
result, err := doGetJSON[Album](pp, "/api/v1/albums/"+uid)

// Generic POST with request body
result, err := doPostJSON[Album](pp, "/api/v1/albums", createReq)

// DELETE with no response body
err := doDelete(pp, "/api/v1/labels/"+uid)
```

### Data Flow

1. CLI command invokes sorter with album UID
2. Sorter fetches photos via PhotoPrism client
3. Each photo is downloaded and sent to AI provider for analysis
4. AI suggests categories/labels
5. Labels are applied back to PhotoPrism (unless dry-run)

### Configuration

Environment variables (loaded from `.env`):
- `PHOTOPRISM_URL`, `PHOTOPRISM_USERNAME`, `PHOTOPRISM_PASSWORD`
- `PHOTOPRISM_DOMAIN` (optional, public URL for generating clickable photo links, e.g., `https://photos.example.com`)
- `OPENAI_TOKEN`
- `GEMINI_API_KEY`
- `OLLAMA_URL` (optional, defaults to http://localhost:11434)
- `OLLAMA_MODEL` (optional, defaults to llama3.2-vision:11b)
- `LLAMACPP_URL` (optional, defaults to http://localhost:8080)
- `LLAMACPP_MODEL` (optional, defaults to llava)
- `EMBEDDING_URL` (optional, defaults to http://localhost:8000)
- `EMBEDDING_DIM` (optional, defaults to 768)
- `DATABASE_URL` (required, e.g., `postgres://user:pass@host:5432/photosorter?sslmode=disable`)
- `DATABASE_MAX_OPEN_CONNS` (optional, defaults to 25)
- `DATABASE_MAX_IDLE_CONNS` (optional, defaults to 5)
- `HNSW_INDEX_PATH` (optional, path to persist face HNSW index, e.g., `/data/faces.pg.hnsw`)
- `HNSW_EMBEDDING_INDEX_PATH` (optional, path to persist embedding HNSW index, e.g., `/data/embeddings.pg.hnsw`)

### AI Provider API Calls

For **standard mode** (non-batch) with N images:

**Default (album-wide date estimation):**
```
1..N  AnalyzePhoto(imageN)              → labels + description (N calls, parallel)
N+1   EstimateAlbumDate(all descriptions) → single date for album (1 call)
Total: N+1 API calls (N run in parallel with --concurrency workers)
```

**With `--individual-dates` flag:**
```
1..N  AnalyzePhoto(imageN) → labels + description + date (N calls, parallel)
Total: N API calls (date estimation included in each analysis)
```

**Batch mode (`--batch` flag):**
```
1. CreatePhotoBatch(all images) → submit batch job (1 call)
2. GetBatchStatus() → poll until complete (multiple calls, every 5s)
3. GetBatchResults() → download results (1 call)
4. EstimateAlbumDate() → if not using individual dates (1 call)
```

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
# List all labels (sorted by name)
go run . labels

# Sort by photo count (descending)
go run . labels --sort=-count

# Only show labels with at least 5 photos
go run . labels --min-photos=5

# Delete labels by UID
go run . labels delete <uid1> <uid2> ...

# Delete without confirmation
go run . labels delete --yes <uid>
```

### Upload Command

```bash
go run . upload <album-uid> <folder-path> [folder-path...] [flags]

Flags:
  -r, --recursive      Search for photos recursively in subdirectories (default: flat search)
```

Examples:
```bash
# Upload from a single folder (flat search)
go run . upload aq8i4k2l3m9n0o1p /path/to/photos

# Upload from multiple folders
go run . upload aq8i4k2l3m9n0o1p /path/to/folder1 /path/to/folder2

# Recursive search in subdirectories
go run . upload -r aq8i4k2l3m9n0o1p /path/to/photos
```

### Move Command

```bash
go run . move <source-album-uid> <new-album-name>
```

Moves all photos from a source album to a newly created album. After successful move, the source album will be empty.

Example:
```bash
go run . move aq8i4k2l3m9n0o1p "Vacation 2024"
```

### Photo Info Command

```bash
go run . photo info <photo-uid> [flags]

Flags:
  --album <uid>     Process all photos in an album instead of single photo
  --json            Output as JSON (default: human-readable)
  --limit N         Limit number of photos in album mode (0 = no limit)
  --concurrency N   Number of parallel workers (default 5)
```

Computes perceptual hashes (pHash and dHash) for photos, useful for:
- Finding duplicate or near-duplicate images
- Similarity matching (using Hamming distance)
- Photo classification pipelines

Examples:
```bash
# Single photo info
go run . photo info pq8abc123def

# All photos in album as JSON
go run . photo info --album aq8xyz789ghi --json

# Process with limited concurrency
go run . photo info --album aq8xyz789ghi --concurrency 3
```

**Hash algorithms:**
- **pHash (perceptual hash)**: DCT-based, robust to resizing and minor edits
- **dHash (difference hash)**: Gradient-based, fast computation

Hamming distance ≤ 10 typically indicates near-duplicate images.

### Cache Sync Command

```bash
go run . cache sync [flags]

Flags:
  --concurrency N   Number of parallel workers (default 20)
  --json            Output as JSON instead of progress bar
```

Syncs face marker data from PhotoPrism to the local PostgreSQL cache. Useful when faces are assigned/unassigned directly in PhotoPrism's native UI.

Examples:
```bash
# Run sync with default concurrency
go run . cache sync

# Limit concurrency for slower systems
go run . cache sync --concurrency 5

# JSON output for scripting
go run . cache sync --json
```

### Database Package

The `internal/database/` package provides storage for embeddings and faces data using PostgreSQL with pgvector.

**In-Memory HNSW:**

The PostgreSQL backend loads both face embeddings and image embeddings into separate in-memory HNSW indexes at server startup for O(log N) similarity search. The face index is automatically updated when faces are saved via the API.

**HNSW Index Persistence:**

By default, the in-memory HNSW indexes are rebuilt from PostgreSQL on every startup. To enable fast startup, set the index paths to persist to disk:

```env
HNSW_INDEX_PATH=/data/faces.pg.hnsw
HNSW_EMBEDDING_INDEX_PATH=/data/embeddings.pg.hnsw
```

The indexes are automatically saved on graceful shutdown (Ctrl+C) and after "Rebuild Index" operations. On startup, they load from disk if the index is fresh (count matches the database), otherwise rebuild from scratch.

**Disk files (faces):**
```
faces.pg.hnsw        # HNSW graph structure (~50-100MB)
faces.pg.hnsw.meta   # Freshness metadata (~100 bytes JSON)
faces.pg.hnsw.faces  # Face metadata + embeddings (~230MB gob for 112k faces)
```

**Disk files (embeddings):**
```
embeddings.pg.hnsw            # HNSW graph structure
embeddings.pg.hnsw.meta       # Freshness metadata (~100 bytes JSON)
embeddings.pg.hnsw.embeddings # Embedding data (gob encoded)
```

The `.faces` and `.embeddings` files store metadata alongside embeddings, enabling fast startup without querying PostgreSQL. If the metadata files are missing but `.hnsw` exists (old format), it falls back to loading from PostgreSQL with a warning.

**Key files:**
```
internal/database/
├── types.go            # StoredPhoto, StoredFace, ExportData structs
├── repository.go       # FaceReader, FaceWriter, EmbeddingReader interfaces
├── provider.go         # Provider functions for getting readers/writers
├── hnsw_index.go       # HNSW index for face embeddings (int64 keys)
├── hnsw_embeddings.go  # HNSW index for image embeddings (string keys)
├── constants.go        # Shared constants (face size thresholds, HNSW params)
└── postgres/           # PostgreSQL backend
    ├── postgres.go     # Connection pool management
    ├── migrations.go   # Auto-apply migrations on startup
    ├── embeddings.go   # EmbeddingReader implementation with pgvector
    ├── faces.go        # FaceReader/FaceWriter implementation with pgvector
    └── migrations/     # SQL migration files (embedded)
```

**PostgreSQL Schema:**
```sql
-- Image embeddings (768-dim CLIP)
CREATE TABLE embeddings (
    photo_uid VARCHAR(32) PRIMARY KEY,
    embedding VECTOR(768) NOT NULL,
    model VARCHAR(64) NOT NULL,
    pretrained VARCHAR(64) NOT NULL,
    dim INTEGER NOT NULL DEFAULT 768,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Face embeddings (512-dim ResNet100)
CREATE TABLE faces (
    id BIGSERIAL PRIMARY KEY,
    photo_uid VARCHAR(32) NOT NULL,
    face_index INTEGER NOT NULL,
    embedding VECTOR(512) NOT NULL,
    bbox DOUBLE PRECISION[4] NOT NULL,
    det_score DOUBLE PRECISION NOT NULL,
    -- Cached PhotoPrism data
    marker_uid VARCHAR(32),
    subject_uid VARCHAR(32),
    subject_name VARCHAR(255),
    photo_width INTEGER,
    photo_height INTEGER,
    orientation INTEGER,
    file_uid VARCHAR(32),
    UNIQUE(photo_uid, face_index)
);

-- HNSW indexes (auto-updating on INSERT)
CREATE INDEX idx_embeddings_vector ON embeddings
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 200);
CREATE INDEX idx_faces_vector ON faces
    USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 200);
```

**StoredFace struct:**
```go
type StoredFace struct {
    ID         int64       // Unique face ID
    PhotoUID   string      // Parent photo UID
    FaceIndex  int         // Index within the photo (0, 1, 2...)
    Embedding  []float32   // 512-dim face embedding (ResNet100)
    BBox       []float64   // [x1, y1, x2, y2] in pixels
    DetScore   float64     // Detection confidence (0-1)

    // Cached PhotoPrism data (populated during processing)
    MarkerUID   string  // Matching PhotoPrism marker UID
    SubjectUID  string  // Subject UID from marker
    SubjectName string  // Person name from marker (e.g., "john-doe")
    PhotoWidth  int     // Primary file width in pixels
    PhotoHeight int     // Primary file height in pixels
    Orientation int     // EXIF orientation (1-8)
    FileUID     string  // Primary file UID
}
```

The cached fields eliminate API calls during face suggestion computation (from ~200 calls/face to 0).

**HNSW Index:**

In-memory approximate nearest neighbor index for O(log N) similarity search. Parameters in `constants.go`:
```go
const (
    HNSWMaxNeighbors    = 16   // M - max neighbors per node
    HNSWEfSearch        = 100  // Search candidate pool
    HNSWEfConstruction  = 200  // Build-time quality
    HNSWSearchMultiplier = 3   // Request 3x candidates, filter by actual distance
)
```

**FaceWriter interface (for cache updates):**
```go
type FaceWriter interface {
    FaceReader
    SaveFaces(ctx context.Context, photoUID string, faces []StoredFace) error
    MarkFacesProcessed(ctx context.Context, photoUID string, faceCount int) error
    UpdateFaceMarker(ctx context.Context, photoUID string, faceIndex int, markerUID, subjectUID, subjectName string) error
    UpdateFacePhotoInfo(ctx context.Context, photoUID string, width, height, orientation int, fileUID string) error
}
```

The `UpdateFaceMarker` method keeps the cache synchronized when faces are assigned via the UI.

**Cache Synchronization:**

The cache stays in sync automatically when faces are assigned through Photo Sorter's UI. However, if faces are assigned/unassigned directly in PhotoPrism's native UI, the cache becomes stale. Use the **Sync Cache** feature (Tools → Process → Sync Cache) to re-sync:

- Scans all photos with faces in the database
- Fetches current marker data from PhotoPrism
- Updates cached `marker_uid`, `subject_uid`, `subject_name`, dimensions, and orientation
- Only updates faces where data has changed

**Face Name Normalization:**

The `GetFacesBySubjectName` method normalizes names before comparison to handle format differences between slugs (from frontend) and display names (stored in database):

```
Input slug:    "jan-novak"   → normalized: "jan novak"
Stored name:   "Jan Novák"   → normalized: "jan novak"
```

Normalization (via `facematch.NormalizePersonName`):
1. Remove diacritics (á → a, ů → u, etc.)
2. Convert to lowercase
3. Replace dashes with spaces

For PostgreSQL, this uses the `unaccent` extension (migration `005_create_unaccent.sql`).

### AI Prompts

Located in `internal/ai/prompts/` (embedded at compile time):
- `photo_analysis.txt` - Labels + description only
- `photo_analysis_with_date.txt` - Labels + description + date estimation
- `album_date.txt` - Album-wide date estimation from descriptions
- `clip_translate.txt` - Czech to CLIP-optimized English translation for text search

**Language:** Czech (descriptions are generated in Czech)

**Location context:** Prompts assume photos are from Veselice, Czech Republic (Jihomoravský kraj, near Moravský kras)

**Metadata:** Prompts instruct AI to use provided metadata (filename, EXIF date, GPS) for better analysis

### Pricing Configuration

Model prices are in `internal/config/prices.yaml` (embedded at compile time):
```yaml
models:
  gpt-4.1-mini:
    standard:
      input: 0.40   # per 1M tokens
      output: 1.60
    batch:
      input: 0.20
      output: 0.80
  gemini-2.5-flash:
    standard:
      input: 0.30
      output: 2.50
    batch:
      input: 0.15
      output: 1.25
  llama3.2-vision:  # Ollama (local, free)
    standard:
      input: 0.00
      output: 0.00
    batch:
      input: 0.00
      output: 0.00
  llava:  # llama.cpp (local, free)
    standard:
      input: 0.00
      output: 0.00
    batch:
      input: 0.00
      output: 0.00
```

### Metadata Behavior

When applying AI results to PhotoPrism:
- **Labels:** Replaced with AI-suggested labels (confidence > 80%)
- **Description/Caption:** Always regenerated (includes AI model info)
- **Date (TakenAt):** Only set if photo has no existing date (Year = 0 or 1), unless `--force-date` is used
- **Notes:** Updated with "Analyzed by: <model>"

Existing EXIF dates are preserved - AI date estimation only fills gaps. Use `--force-date` to overwrite incorrect dates.

### PhotoPrism API Documentation

PhotoPrism API swagger spec is at `internal/photoprism/swagger.yaml`. Reference this when implementing new API methods.

**API Quirk:** `GetAlbum()` (single album endpoint `/albums/{uid}`) does NOT return `PhotoCount` - it only appears in the list endpoint (`/albums`). Do not rely on `PhotoCount` from single album responses; fetch photos directly to determine if an album is empty.

### API Response Capturing

Use `--capture <dir>` flag to save API responses for testing:
```bash
go run . --capture ./testdata albums
```

Test fixtures are stored in `internal/photoprism/testdata/` following Go conventions.

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
- `WEB_SESSION_SECRET` - Secret for signing session cookies

**Session Persistence:**

Sessions are persisted to PostgreSQL, allowing users to remain logged in across server restarts. The implementation uses a write-through cache pattern:

- On login: session saved to both in-memory map and PostgreSQL
- On request: memory checked first, then database (restored to memory if found)
- On logout: session removed from both memory and database
- Background cleanup: expired sessions removed every 10 minutes

Schema (`internal/database/postgres/migrations/006_create_sessions.sql`):
```sql
CREATE TABLE sessions (
    id VARCHAR(255) PRIMARY KEY,
    token TEXT NOT NULL,
    download_token TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);
```

Key files:
- `internal/database/postgres/sessions.go` - PostgreSQL repository
- `internal/web/middleware/session.go` - SessionManager with persistence support

**API Endpoints:**
- `POST /api/v1/auth/login` - Login with PhotoPrism credentials
- `GET /api/v1/auth/status` - Check authentication status
- `GET /api/v1/albums` - List albums
- `GET /api/v1/albums/:uid/photos` - Get photos in album (no quality filter)
- `GET /api/v1/labels` - List labels
- `GET /api/v1/labels/:uid` - Get single label
- `PUT /api/v1/labels/:uid` - Update label (rename, etc.)
- `DELETE /api/v1/labels` - Batch delete labels
- `POST /api/v1/sort` - Start AI sort job
- `GET /api/v1/sort/:jobId/events` - SSE stream for job progress
- `POST /api/v1/upload` - Upload photos (multipart)
- `GET /api/v1/config` - Get available providers
- `GET /api/v1/stats` - Get processing statistics (processed count uses OR of embeddings/faces-processed per photo)
- `GET /api/v1/photos/:uid/faces` - Get faces in a photo with suggestions
- `POST /api/v1/photos/:uid/faces/compute` - Compute face embeddings for a photo
- `GET /api/v1/subjects/:uid` - Get single subject
- `PUT /api/v1/subjects/:uid` - Update subject (rename, etc.)
- `POST /api/v1/faces/match` - Find photos matching a person's face
- `POST /api/v1/faces/apply` - Apply face match (create/assign/unassign marker)
- `POST /api/v1/faces/outliers` - Detect face outliers for a person
- `POST /api/v1/photos/search-by-text` - Text-to-image similarity search (auto-translates Czech to CLIP-optimized English via GPT-4.1-mini if `OPENAI_TOKEN` set; returns `translated_query` and `translate_cost_usd`)
- `POST /api/v1/process` - Start photo processing job (embeddings + face detection)
- `GET /api/v1/process/{jobId}/events` - SSE stream for process job progress
- `DELETE /api/v1/process/{jobId}` - Cancel process job
- `POST /api/v1/process/rebuild-index` - Rebuild HNSW indexes and reload in memory
- `POST /api/v1/process/sync-cache` - Sync face marker data from PhotoPrism to local cache

**Frontend Structure:**
```
web/src/
├── api/
│   └── client.ts              # Typed API client
├── components/                # Shared UI components
│   ├── Button.tsx
│   ├── Card.tsx
│   ├── ErrorBoundary.tsx      # Error catching wrapper
│   ├── FormCheckbox.tsx       # Styled checkbox with label
│   ├── FormInput.tsx          # Styled text/number input with label
│   ├── FormSelect.tsx         # Styled select dropdown with label
│   ├── LazyImage.tsx
│   ├── Layout.tsx
│   ├── LoadingState.tsx       # Unified loading/error/empty states
│   ├── PhotoCard.tsx
│   ├── PhotoGrid.tsx
│   ├── PhotoWithBBox.tsx
│   └── StatsGrid.tsx          # Stats display grid (configurable columns/colors)
├── constants/                 # Shared constants
│   ├── actions.ts             # Face action styling (labels, colors)
│   └── index.ts               # Magic numbers and defaults
├── hooks/                     # Global hooks
│   ├── useAuth.tsx
│   ├── useFaceApproval.ts     # Face approval logic (single + batch)
│   ├── useSSE.ts              # Server-Sent Events
│   └── useSubjectsAndConfig.ts # Shared data loading
├── pages/                     # Page components (folder-based)
│   ├── Analyze/               # AI analysis page
│   │   ├── hooks/useSortJob.ts
│   │   ├── AnalyzeForm.tsx
│   │   ├── AnalyzeResults.tsx
│   │   ├── AnalyzeStatus.tsx
│   │   └── index.tsx
│   ├── Faces/                 # Face matching page
│   │   ├── hooks/useFaceSearch.ts
│   │   ├── FacesConfigPanel.tsx
│   │   ├── FacesMatchGrid.tsx
│   │   ├── FacesResultsSummary.tsx
│   │   └── index.tsx
│   ├── Photos/                # Photo browsing page
│   │   ├── hooks/usePhotosFilters.ts
│   │   ├── hooks/usePhotosPagination.ts
│   │   ├── PhotosFilters.tsx
│   │   └── index.tsx
│   ├── PhotoDetail/           # Photo detail page
│   │   ├── hooks/
│   │   ├── EmbeddingsStatus.tsx
│   │   ├── FaceAssignmentPanel.tsx
│   │   ├── FacesList.tsx
│   │   ├── PhotoDisplay.tsx
│   │   └── index.tsx
│   └── Recognition/           # Bulk face recognition
│       ├── hooks/useScanAll.ts
│       ├── PersonResultCard.tsx
│       ├── ScanConfigPanel.tsx
│       ├── ScanResultsSummary.tsx
│       └── index.tsx
└── types/
    ├── events.ts              # Typed SSE events
    └── index.ts               # API response types
```

**Shared Hooks:**

- `useFaceApproval` - Handles single and batch face approval with progress tracking. Used by Faces, Recognition, and PhotoDetail pages.
- `useSubjectsAndConfig` - Loads subjects (people) and config in parallel. Used by Faces, Recognition, and Outliers pages.
- `useSSE` - Server-Sent Events hook for real-time job progress. Used by Analyze and Process pages.

**Typed SSE Events:**

SSE events use discriminated unions in `types/events.ts` for type safety:
```typescript
export type SortJobEvent =
  | { type: 'status'; data: SortJob }
  | { type: 'progress'; data: { processed_photos: number; total_photos: number } }
  | { type: 'completed'; data: SortJobResult }
  | { type: 'job_error'; message: string };
```

Use `parseSortJobEvent()` and `parseProcessJobEvent()` helpers to safely parse raw SSE messages.

**Action Constants:**

Face action styling is centralized in `constants/actions.ts`:
```typescript
import { ACTION_LABELS, ACTION_BORDER_COLORS, ACTION_BG_COLORS } from '../constants/actions';
```

**Error Handling:**

The app is wrapped in `ErrorBoundary` which catches React rendering errors and displays a user-friendly error page with retry options.

**Loading States:**

Use `LoadingState` for consistent loading/error/empty states:
```typescript
<LoadingState isLoading={loading} error={error} isEmpty={data.length === 0}>
  {/* Content when loaded */}
</LoadingState>
```

**Form Components:**

Use shared form components for consistent styling:
```typescript
import { FormInput } from '../components/FormInput';
import { FormSelect } from '../components/FormSelect';
import { FormCheckbox } from '../components/FormCheckbox';

// Text/number input with label
<FormInput
  label="Limit (0 = no limit)"
  type="number"
  value={limit}
  onChange={(e) => setLimit(parseInt(e.target.value) || 0)}
  disabled={isRunning}
  min={0}
/>

// Select dropdown with label
<FormSelect
  label="Album"
  value={selectedAlbum}
  onChange={(e) => setSelectedAlbum(e.target.value)}
  disabled={isRunning}
>
  <option value="">Select an album...</option>
  {albums.map((a) => <option key={a.uid} value={a.uid}>{a.title}</option>)}
</FormSelect>

// Checkbox with label
<FormCheckbox
  label="Dry run (preview only)"
  checked={dryRun}
  onChange={(e) => setDryRun(e.target.checked)}
  disabled={isRunning}
/>
```

**Stats Grid:**

Use `StatsGrid` for consistent stats display:
```typescript
import { StatsGrid } from '../components/StatsGrid';

<StatsGrid
  columns={3}  // 2, 3, or 4 columns
  items={[
    { value: totalCount, label: 'Total' },
    { value: successCount, label: 'Success', color: 'green' },
    { value: errorCount, label: 'Errors', color: 'red' },
  ]}
/>
```

Available colors: `white` (default), `blue`, `green`, `yellow`, `orange`, `red`.

**Handler Structure:**

Face-related handlers are split across multiple files for maintainability:
```
internal/web/handlers/
├── faces.go           # FacesHandler struct and constructor
├── subjects.go        # Subject CRUD (ListSubjects, GetSubject, UpdateSubject)
├── face_match.go      # Face matching and similarity search (Match)
├── face_apply.go      # Applying face matches (Apply, ComputeFaces)
├── face_outliers.go   # Outlier detection (FindOutliers)
├── face_photos.go     # Photo face retrieval and suggestions (GetPhotoFaces)
└── face_helpers.go    # Shared helpers (delegating to facematch package)
```

**PhotoPrism Client Middleware:**

Handlers use middleware to get the PhotoPrism client from context:
```go
// In handler methods - use MustGetPhotoPrism helper
func (h *AlbumsHandler) List(w http.ResponseWriter, r *http.Request) {
    pp := middleware.MustGetPhotoPrism(r.Context(), w)
    if pp == nil {
        return  // Error response already sent
    }
    // Use pp...
}

// For background goroutines that outlive the request, get session first
func (h *SortHandler) Start(w http.ResponseWriter, r *http.Request) {
    pp := middleware.MustGetPhotoPrism(r.Context(), w)
    if pp == nil {
        return
    }
    session := middleware.GetSessionFromContext(r.Context())
    // ... later in goroutine:
    go h.runSortJob(job, session)  // Pass session, create client inside goroutine
}
```

The frontend is embedded in the Go binary at compile time via `go:embed`. Run `make build` to build both frontend and backend into a single binary.

**Common Pitfall - Bounding Box Positioning:**
When rendering bounding boxes (face boxes, etc.) as absolute-positioned overlays on images, the parent container MUST have `position: relative`. Otherwise, percentage-based coordinates will be relative to the wrong ancestor, causing misalignment when images have different sizes.

```tsx
// Wrong - bbox positioned relative to outer container
<div className="...">
  <img ... />
  <div className="absolute" style={{ left: '10%', top: '20%' }} />
</div>

// Correct - bbox positioned relative to image container
<div className="relative ...">
  <img ... />
  <div className="absolute" style={{ left: '10%', top: '20%' }} />
</div>
```

**Common Pitfall - Subject/Album Thumbnail Hashes:**
The `Thumb` field on `Subject` and `Album` structs is a **file hash**, not a photo UID. It cannot be used with `getThumbnailUrl(uid, size)` which generates `/api/v1/photos/{uid}/thumb/{size}` (expects a photo UID). Using the hash as a UID produces a broken image, and if `alt` text contains the entity name, the browser displays the name as fallback text — making it appear duplicated next to the actual heading.

```tsx
// Wrong - subject.thumb is a file hash, not a photo UID
<img src={getThumbnailUrl(subject.thumb, 'tile_50')} alt={subject.name} />

// Correct - use a fallback icon instead
<div className="h-10 w-10 rounded-full bg-slate-700 flex items-center justify-center">
  <User className="h-5 w-5 text-slate-400" />
</div>
```

### Photo Faces API

The `GET /api/v1/photos/:uid/faces` endpoint returns all faces detected in a photo, combining data from two sources:

1. **Embeddings database (PostgreSQL)** - faces detected by InsightFace (buffalo_l/ResNet100), stored with 512-dim embeddings
2. **PhotoPrism markers** - faces detected by PhotoPrism's built-in face detection

The response includes counts from both sources (`embeddings_count`, `markers_count`) to surface discrepancies. Faces are matched between the two sources using IoU (threshold >= 0.1) in display coordinate space.

**Minimum face size:** The `GetPhotoFaces` endpoint does NOT filter by face size — it returns all detected faces regardless of dimensions, since the photo detail page is for manual inspection. The `Match` endpoint (`POST /api/v1/faces/match`) still applies minimum size filtering using constants from `internal/database/constants.go` (`MinFaceWidthPx = 35`, `MinFaceWidthRel = 0.01`). PhotoPrism's own face detection minimum is configured to 30px.

**Unmatched markers:** If PhotoPrism has face markers that don't match any database embedding (e.g., 3 markers but only 2 embeddings), the unmatched markers are appended to the response with:
- `face_index`: negative values (-1, -2, ...) to distinguish from database faces
- `bbox_rel`: from the marker's relative coordinates (display space)
- `bbox`: empty (no pixel coordinates available)
- No suggestions (no embedding to compare against)

Response:
```json
{
  "photo_uid": "pq8abc...",
  "file_uid": "fq8xyz...",
  "width": 4000,
  "height": 3000,
  "orientation": 1,
  "embeddings_count": 2,
  "markers_count": 3,
  "faces": [
    {
      "face_index": 0,
      "bbox": [100, 50, 300, 280],
      "bbox_rel": [0.025, 0.017, 0.05, 0.077],
      "det_score": 0.95,
      "marker_uid": "mq8def...",
      "marker_name": "jan-novak",
      "action": "already_done",
      "suggestions": []
    },
    {
      "face_index": -1,
      "bbox_rel": [0.5, 0.3, 0.08, 0.1],
      "action": "assign_person",
      "suggestions": []
    }
  ]
}
```

### Face Outlier Detection

The Outliers page (`/outliers`) detects wrongly assigned faces by computing the centroid of a person's face embeddings and ranking each face by cosine distance from that centroid.

**Algorithm:**
1. Fetch all photos tagged with `person:<name>` from PhotoPrism (paginated)
2. For each photo (parallelized with 20 workers):
   - Get face embeddings from the PostgreSQL database
   - Get markers from PhotoPrism to find the person's marker
   - Get photo dimensions to handle orientation-aware coordinate conversion
   - Match marker to database face using IoU in display coordinate space
3. Compute centroid (element-wise mean of all 512-dim face embeddings)
4. Compute cosine distance from centroid for each face
5. Sort by distance descending (most suspicious first)
6. Apply threshold/limit filters

**API: `POST /api/v1/faces/outliers`**

Request:
```json
{
  "person_name": "jan-novak",
  "threshold": 0.15,
  "limit": 50
}
```

Response:
```json
{
  "person": "jan-novak",
  "total_faces": 200,
  "avg_distance": 0.08,
  "outliers": [
    {
      "photo_uid": "pq8abc...",
      "dist_from_centroid": 0.45,
      "face_index": 0,
      "bbox_rel": [0.1, 0.05, 0.1, 0.13],
      "file_uid": "fq8xyz...",
      "marker_uid": "mq8def..."
    }
  ],
  "missing_embeddings": [
    {
      "photo_uid": "pq8ghi...",
      "dist_from_centroid": -1,
      "face_index": -1,
      "bbox_rel": [0.3, 0.2, 0.08, 0.1],
      "marker_uid": "mq8jkl..."
    }
  ]
}
```

**Missing embeddings:** Faces assigned to the person in PhotoPrism but without a matching embedding in the database. These have `face_index: -1` and `dist_from_centroid: -1` since they cannot be compared to the centroid. They are shown separately in the UI and can still be unassigned.

**Unassigning faces:** The `POST /api/v1/faces/apply` endpoint supports `action: "unassign_person"` which calls `ClearMarkerSubject` to remove the person assignment from a marker.

**Coordinate handling:** Marker bounding boxes from PhotoPrism are in display space (orientation-adjusted relative coordinates). The embedding service (InsightFace) auto-rotates images based on EXIF orientation before face detection, so database face bboxes are also in display space (but in pixels). PhotoPrism reports raw file dimensions, so for orientations 5-8 (90° rotations), dimensions must be swapped to get display dimensions before converting pixel coords to relative coords. The `convertPixelBBoxToDisplayRelative` function handles this dimension swap and converts pixel coordinates to relative (0-1) coordinates for display.

### Recognition Page

The Recognition page (`/recognition`) scans all known people for high-confidence face matches across the entire photo library. It uses the existing `POST /api/v1/faces/match` endpoint — no new backend endpoints required.

**How it works:**
1. Loads all subjects with `photo_count > 0`
2. On "Scan All People", iterates through each subject calling `matchFaces` with concurrency of 3
3. Filters out `already_done` matches, only showing actionable ones (`create_marker` or `assign_person`)
4. Results stream into the UI as each person completes (grouped by person)
5. Each person section has its own "Accept All" button

**Confidence → distance mapping:**
```typescript
const distanceThreshold = 1 - confidence / 100;
// 70% → 0.30, 80% → 0.20, 95% → 0.05
```

**Performance considerations (20k+ photo library):**
- Concurrency limited to 3 parallel `matchFaces` requests
- Only scans subjects with `photo_count > 0`
- Cancellable via ref flag (stops workers from picking up new subjects)
- Results appear incrementally as each person completes

**State management:**
- `PersonResult[]` — array of `{ slug, name, actionable: FaceMatch[], alreadyDone: number }`
- On approve/reject, person entries with 0 remaining actionable matches are filtered out
- Accept All snapshots the actionable list before starting to avoid mutation issues

## Documentation Requirements

**IMPORTANT:** Keep documentation updated with every code change.

When adding or modifying features, update the relevant documentation:

- **`docs/cli-reference.md`** - Update when adding/changing CLI commands or flags
- **`docs/web-ui.md`** - Update when adding/changing Web UI pages or features
- **`docs/postgresql-migration.md`** - Update when changing database schema or migration process
- **`docs/markers.md`** - Update when changing marker/face matching logic or coordinate handling
- **`README.md`** - Update for major feature additions or architectural changes

Documentation files:
```
docs/
├── cli-reference.md        # Complete CLI command reference
├── web-ui.md               # Web UI features and API endpoints
├── postgresql-migration.md # PostgreSQL setup and migration guide
└── markers.md              # Marker system and face-to-marker matching
```
