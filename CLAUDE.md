# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

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

# Run tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run a single test
go test -v ./internal/photoprism/ -run TestGetAlbum

# Lint Go code
make lint

# Lint and auto-fix
make lint-fix

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

- **cmd/** - Cobra commands (albums, labels, count, move, sort, upload, photo, photo info/match/similar/clear-faces, cache sync/compute-eras/push-embeddings, create, clear, version, serve)
- **internal/photoprism/** - PhotoPrism REST API client (split by domain: albums, photos, labels, markers, subjects, upload)
- **internal/ai/** - AI provider interface with OpenAI, Gemini, Ollama, and llama.cpp implementations
- **internal/sorter/** - Orchestrates photo fetching → AI analysis → label application
- **internal/config/** - Environment-based configuration loader
- **internal/fingerprint/** - Perceptual hash computation (pHash, dHash), image/face embeddings client
- **internal/database/** - PostgreSQL storage with pgvector for embeddings and faces data
- **internal/facematch/** - Face matching utilities (IoU computation, bbox conversion, name normalization, marker matching)
- **internal/constants/** - Shared constants (page sizes, thresholds, concurrency, upload limits)
- **internal/web/** - Web server with Chi router, REST API handlers, and SSE for real-time updates
- **web/** - React + TypeScript + TailwindCSS frontend (built with Vite, i18n with Czech + English)

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
result, err := doGetJSON[Album](pp, "/api/v1/albums/"+uid)
result, err := doPostJSON[Album](pp, "/api/v1/albums", createReq)
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
- `PHOTOPRISM_DOMAIN` (optional, public URL for generating clickable photo links)
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
- `PHOTOPRISM_DATABASE_URL` (optional, MariaDB DSN for direct database access, e.g., `photoprism:photoprism@tcp(mariadb:3306)/photoprism`)

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
go run . cache sync [flags]                   # Sync face markers from PhotoPrism to local cache
  --concurrency N   Parallel workers (default 20)
  --json            Output as JSON

go run . cache push-embeddings [flags]        # Push InsightFace embeddings to PhotoPrism MariaDB
  --dry-run               Preview changes
  --recompute-centroids   Recompute face cluster centroids
  --json                  Output as JSON

go run . cache compute-eras [flags]           # Compute CLIP era embedding centroids
  --dry-run   Preview without saving
  --json      Output as JSON
```

### Database Package

The `internal/database/` package provides storage for embeddings and faces data using PostgreSQL with pgvector.

**In-Memory HNSW:** Loads face embeddings (512-dim) and image embeddings (768-dim) into separate in-memory HNSW indexes at startup for O(log N) similarity search. Face index auto-updates when faces are saved. Persistence via `HNSW_INDEX_PATH` / `HNSW_EMBEDDING_INDEX_PATH` env vars; saved on shutdown and after "Rebuild Index".

**Key files:**
```
internal/database/
├── types.go            # StoredPhoto, StoredFace, ExportData, PhotoBook, BookSection, etc.
├── repository.go       # FaceReader, FaceWriter, EmbeddingReader, BookReader, BookWriter interfaces
├── provider.go         # Provider functions for getting readers/writers
├── hnsw_index.go       # HNSW index for face embeddings (int64 keys)
├── hnsw_embeddings.go  # HNSW index for image embeddings (string keys)
├── cosine.go           # Cosine distance computation
├── constants.go        # Shared constants (face size thresholds, HNSW params)
└── postgres/           # PostgreSQL backend
    ├── postgres.go     # Connection pool management
    ├── migrations.go   # Auto-apply migrations on startup
    ├── embeddings.go   # EmbeddingReader implementation with pgvector
    ├── faces.go        # FaceReader/FaceWriter implementation with pgvector
    ├── era_embeddings.go  # EraEmbeddingReader/Writer implementation
    ├── books.go        # BookRepository (BookReader/BookWriter)
    ├── sessions.go     # Session persistence for web auth
    └── migrations/     # SQL migrations 001-011 (embedded)
```

**Tables:** `embeddings` (768-dim CLIP), `faces` (512-dim ResNet100 with cached PhotoPrism marker data), `era_embeddings` (768-dim CLIP text centroids), `faces_processed` (tracking), `sessions`, `photo_books`, `book_sections`, `section_photos`, `book_pages` (with `split_position` for adjustable column splits in mixed formats), `page_slots` (with `text_content` for text-only slots, `crop_x`/`crop_y`/`crop_scale` for per-photo crop control, mutually exclusive with `photo_uid`).

**Face name normalization:** `GetFacesBySubjectName` normalizes names via `facematch.NormalizePersonName` (remove diacritics, lowercase, dashes→spaces) using the `unaccent` PostgreSQL extension.

**Cache sync:** Stays in sync automatically via UI. If faces assigned in PhotoPrism native UI, use Sync Cache to re-sync. Also cleans up orphaned data for deleted/archived photos.

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

Model prices are in `internal/config/prices.yaml` (embedded at compile time). Supports per-model standard and batch pricing for gpt-4.1-mini, gemini-2.5-flash, llama3.2-vision (Ollama), and llava (llama.cpp).

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
- `WEB_SESSION_SECRET` - Secret for signing session cookies (warns at startup if unset)
- `WEB_ALLOWED_ORIGINS` - Comma-separated CORS allowed origins (localhost always allowed)

Sessions are persisted to PostgreSQL (`sessions` table) for survival across server restarts.

Session cookies use `HttpOnly`, `SameSite=Strict`, and auto-detect `Secure` flag when behind HTTPS (`X-Forwarded-Proto` or direct TLS). Security headers (CSP, X-Content-Type-Options, X-Frame-Options) are set on all responses.

**API Endpoints:**
- `GET /api/v1/health` - Health check (no auth)
- `POST /api/v1/auth/login` - Login with PhotoPrism credentials
- `POST /api/v1/auth/logout` - Logout
- `GET /api/v1/auth/status` - Check authentication status
- `GET /api/v1/albums` - List albums
- `POST /api/v1/albums` - Create album
- `GET /api/v1/albums/{uid}` - Get single album
- `GET /api/v1/albums/{uid}/photos` - Get photos in album (no quality filter)
- `POST /api/v1/albums/{uid}/photos` - Add photos to album
- `DELETE /api/v1/albums/{uid}/photos` - Remove photos from album
- `DELETE /api/v1/albums/{uid}/photos/batch` - Remove specific photos from album (batch)
- `GET /api/v1/labels` - List labels
- `GET /api/v1/labels/{uid}` - Get single label
- `PUT /api/v1/labels/{uid}` - Update label (rename, etc.)
- `DELETE /api/v1/labels` - Batch delete labels
- `GET /api/v1/photos` - List photos
- `GET /api/v1/photos/{uid}` - Get single photo
- `PUT /api/v1/photos/{uid}` - Update photo
- `GET /api/v1/photos/{uid}/thumb/{size}` - Get photo thumbnail
- `GET /api/v1/photos/{uid}/faces` - Get faces in a photo with suggestions
- `POST /api/v1/photos/{uid}/faces/compute` - Compute face embeddings for a photo
- `GET /api/v1/photos/{uid}/estimate-era` - Estimate photo era from CLIP embeddings vs era centroids
- `GET /api/v1/photos/{uid}/books` - Get book/section memberships for a photo
- `POST /api/v1/photos/similar` - Find similar photos by embedding
- `POST /api/v1/photos/similar/collection` - Find similar photos across a collection
- `POST /api/v1/photos/search-by-text` - Text-to-image similarity search (auto-translates Czech via GPT-4.1-mini)
- `POST /api/v1/photos/batch/labels` - Batch add labels to photos
- `POST /api/v1/photos/batch/edit` - Batch edit photos (favorite, private)
- `POST /api/v1/photos/batch/archive` - Archive (soft-delete) photos
- `POST /api/v1/photos/duplicates` - Find near-duplicate photos via embedding similarity
- `POST /api/v1/photos/suggest-albums` - Album completion via HNSW centroid search
- `POST /api/v1/sort` - Start AI sort job
- `GET /api/v1/sort/{jobId}` - Get sort job status
- `GET /api/v1/sort/{jobId}/events` - SSE stream for job progress
- `DELETE /api/v1/sort/{jobId}` - Cancel sort job
- `POST /api/v1/upload` - Upload photos (multipart)
- `GET /api/v1/config` - Get available providers
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
- `POST /api/v1/process/rebuild-index` - Rebuild HNSW indexes and reload in memory
- `POST /api/v1/process/sync-cache` - Sync face marker data from PhotoPrism to local cache
- `GET /api/v1/books` - List all photo books
- `POST /api/v1/books` - Create a new book
- `GET /api/v1/books/{id}` - Get book detail with sections and pages
- `PUT /api/v1/books/{id}` - Update book (title, description)
- `DELETE /api/v1/books/{id}` - Delete book (cascades to sections, pages, slots)
- `POST /api/v1/books/{id}/sections` - Create section in a book
- `PUT /api/v1/books/{id}/sections/reorder` - Reorder sections
- `PUT /api/v1/sections/{id}` - Update section (title)
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
- `GET /api/v1/books/{id}/export-pdf` - Export book as PDF (requires lualatex)

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
│   ├── actions.ts             # Face action styling (labels, colors)
│   ├── index.ts               # Magic numbers and defaults
│   └── pageConfig.ts          # Book page format configuration
├── hooks/                     # Global hooks
│   ├── useAuth.tsx
│   ├── useFaceApproval.ts     # Face approval logic (single + batch)
│   ├── usePhotoSelection.ts   # Shared photo selection + bulk actions
│   ├── useSSE.ts              # Server-Sent Events
│   └── useSubjectsAndConfig.ts # Shared data loading
├── i18n/                      # Internationalization (Czech + English)
│   ├── index.ts
│   └── locales/{cs,en}/       # common.json, forms.json, pages.json
├── utils/
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
│   │   ├── EraEstimate.tsx, BookMembership.tsx, AddToBookDropdown.tsx
│   │   ├── FacesList.tsx, FaceAssignmentPanel.tsx, EmbeddingsStatus.tsx
│   │   └── PhotoDisplay.tsx
│   ├── Recognition/           # Bulk face recognition (hooks/useScanAll.ts)
│   ├── Duplicates/            # Near-duplicate detection
│   ├── Compare/               # Side-by-side comparison (hooks/useCompareState.ts)
│   ├── Books/                 # Photo books list
│   ├── BookEditor/            # Book editor (sections, pages, preview)
│   │   ├── hooks/useBookData.ts
│   │   ├── SectionsTab.tsx, SectionSidebar.tsx, SectionPhotoPool.tsx
│   │   ├── PagesTab.tsx, PageSidebar.tsx, PageTemplate.tsx, PageSlot.tsx
│   │   ├── UnassignedPool.tsx, PreviewTab.tsx
│   │   ├── PhotoBrowserModal.tsx, PhotoDescriptionDialog.tsx
│   │   └── PhotoActionOverlay.tsx, PhotoInfoOverlay.tsx
│   ├── Slideshow/             # Photo slideshow (hooks/useSlideshow.ts, useSlideshowPhotos.ts)
│   ├── SuggestAlbums/         # Album completion
│   └── Help/                  # Help screenshot assets (cs/, en/)
└── types/
    ├── events.ts              # Typed SSE events (discriminated unions)
    └── index.ts               # API response types
```

**Shared Hooks:**
- `useFaceApproval` - Single and batch face approval. Used by Faces, Recognition, PhotoDetail.
- `usePhotoSelection` - Photo selection + bulk actions (add to album, label, favorite, remove). Used by Photos, SimilarPhotos, Expand, Duplicates.
- `useSubjectsAndConfig` - Loads subjects and config in parallel. Used by Faces, Recognition, Outliers.
- `useSSE` - Server-Sent Events for real-time job progress. Used by Analyze and Process.

**Handler Structure:**
```
internal/web/handlers/
├── auth.go, albums.go, labels.go, photos.go   # Core CRUD
├── sort.go, upload.go, process.go              # Jobs
├── config.go, stats.go, sse.go, common.go      # Utilities
├── subjects.go                                 # Subject CRUD
├── faces.go                                    # FacesHandler struct
├── face_match.go, face_apply.go                # Face matching and applying
├── face_outliers.go, face_photos.go            # Outlier detection, photo faces
├── face_helpers.go                             # Shared face helpers
├── books.go                                    # BooksHandler: books, sections, pages, slots
└── jobs.go                                     # Sort job status
```

**Photo Book Database:**

Tables: `photo_books`, `book_sections`, `section_photos`, `book_pages`, `page_slots` (migration 008, extended by 009-013, 015, plus crop/split features). Slots hold either `photo_uid` or `text_content` (mutually exclusive via CHECK constraint) with `crop_x`/`crop_y` for crop positioning (0.0-1.0, default 0.5) and `crop_scale` for zoom level (0.1-1.0, default 1.0). Pages have a `style` field (`modern`/`archival`, migration 013) and `split_position` for adjustable column splits in `2l_1p`/`1p_2l` formats (0.2-0.8, default 0.5).

Page formats: `4_landscape` (4 slots), `2l_1p` (3 slots), `1p_2l` (3 slots), `2_portrait` (2 slots), `1_fullscreen` (1 slot). Layout uses a 12-column grid with 3 fixed zones (header 4mm / canvas 172mm / footer 8mm) and asymmetric margins (inside 20mm / outside 12mm). Mixed formats support adjustable split position via `split_position`.

**PhotoPrism Client Middleware:**

Handlers use `middleware.MustGetPhotoPrism(r.Context(), w)` to get the PhotoPrism client from context. For background goroutines that outlive the request, capture the session first via `middleware.GetSessionFromContext(r.Context())` and create a new client in the goroutine.

The frontend is embedded in the Go binary at compile time via `go:embed`. Run `make build` to build both frontend and backend into a single binary.

**Common Pitfall - Bounding Box Positioning:**
When rendering bounding boxes as absolute-positioned overlays on images, the parent container MUST have `position: relative`. Otherwise, percentage-based coordinates will be relative to the wrong ancestor.

**Common Pitfall - Subject/Album Thumbnail Hashes:**
The `Thumb` field on `Subject` and `Album` structs is a **file hash**, not a photo UID. It cannot be used with `getThumbnailUrl(uid, size)` which expects a photo UID. Use a fallback icon instead.

### Photo Faces API

The `GET /api/v1/photos/:uid/faces` endpoint combines faces from the embeddings database (InsightFace) and PhotoPrism markers, matched via IoU (threshold >= 0.1) in display coordinate space.

**Minimum face size:** `GetPhotoFaces` does NOT filter by face size (for manual inspection). `Match` endpoint applies minimum size filtering (`MinFaceWidthPx = 35`, `MinFaceWidthRel = 0.01` from `constants.go`).

**Unmatched markers:** Appended with negative `face_index` (-1, -2, ...), `bbox_rel` from marker coordinates, no suggestions.

### Face Outlier Detection

Detects wrongly assigned faces by computing the centroid of a person's face embeddings and ranking by cosine distance. Faces with `missing_embeddings` (in PhotoPrism but not in database) have `face_index: -1` and `dist_from_centroid: -1`.

**Coordinate handling:** Both PhotoPrism markers and InsightFace embeddings use display space coordinates. For EXIF orientations 5-8 (90° rotations), raw file dimensions must be swapped for display. The `convertPixelBBoxToDisplayRelative` function handles this.

**Unassigning faces:** `POST /api/v1/faces/apply` with `action: "unassign_person"` calls `ClearMarkerSubject`.

### Recognition Page

Scans all known people for high-confidence face matches. Iterates subjects with `photo_count > 0`, calls `matchFaces` with concurrency 3, filters to actionable matches only (`create_marker` or `assign_person`). Results stream incrementally per person. Confidence maps to distance: `distanceThreshold = 1 - confidence / 100`.

## Documentation Requirements

**IMPORTANT:** Keep documentation updated with every code change.

When adding or modifying features, update the relevant documentation:

- **`docs/cli-reference.md`** - Update when adding/changing CLI commands or flags
- **`docs/web-ui.md`** - Update when adding/changing Web UI pages or features
- **`docs/markers.md`** - Update when changing marker/face matching logic or coordinate handling
- **`docs/era-estimation.md`** - Update when changing era estimation logic, centroids, or UI
- **`docs/photo-book.md`** - Update when changing photo book feature (formats, schema, UI)
- **`docs/API.md`** - Update when changing REST API endpoints
- **`docs/testing-environment.md`** - Update when changing dev/test setup
- **`README.md`** - Update for major feature additions or architectural changes

Documentation files:
```
docs/
├── API.md                  # REST API documentation
├── cli-reference.md        # Complete CLI command reference
├── era-estimation.md       # Era estimation: centroids, API, and UI
├── hnsw-architecture.md    # In-memory HNSW vs pgvector design rationale
├── markers.md              # Marker system and face-to-marker matching
├── photo-book.md           # Photo book planning tool
├── testing-environment.md  # Dev/test environment setup
└── web-ui.md               # Web UI features and API endpoints
```
