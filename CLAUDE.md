# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Denied Files

Do NOT read or access the following files:
- `.envrc` - Contains sensitive environment variables
- `settings.local.json` - Contains local settings with sensitive data

## Build and Test Commands

```bash
# Build
go build ./...

# Run tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run a single test
go test -v ./internal/photoprism/ -run TestGetAlbum

# Run the CLI
go run . <command>
```

## Architecture

This is a CLI tool that sorts photos in PhotoPrism using AI providers. Built with Cobra for CLI and Viper for configuration.

### Core Components

- **cmd/** - Cobra commands (albums, labels, count, move, sort, upload, photo, version)
- **internal/photoprism/** - PhotoPrism REST API client with auth, album/photo CRUD, and response capturing for tests
- **internal/ai/** - AI provider interface with OpenAI, Gemini, Ollama, and llama.cpp implementations
- **internal/sorter/** - Orchestrates photo fetching → AI analysis → label application
- **internal/config/** - Environment-based configuration loader
- **internal/fingerprint/** - Perceptual hash computation (pHash, dHash), image/face embeddings client
- **internal/database/** - PostgreSQL repositories for embeddings and faces tables (pgvector)

### Data Flow

1. CLI command invokes sorter with album UID
2. Sorter fetches photos via PhotoPrism client
3. Each photo is downloaded and sent to AI provider for analysis
4. AI suggests categories/labels
5. Labels are applied back to PhotoPrism (unless dry-run)

### Configuration

Environment variables (loaded from `.env`):
- `PHOTOPRISM_URL`, `PHOTOPRISM_USERNAME`, `PHOTOPRISM_PASSWORD`
- `OPENAI_TOKEN`
- `GEMINI_API_KEY`
- `OLLAMA_URL` (optional, defaults to http://localhost:11434)
- `OLLAMA_MODEL` (optional, defaults to llama3.2-vision:11b)
- `LLAMACPP_URL` (optional, defaults to http://localhost:8080)
- `LLAMACPP_MODEL` (optional, defaults to llava)
- `EMBEDDING_URL` (optional, defaults to http://100.94.61.29:8000)
- `EMBEDDING_DIM` (optional, defaults to 768)
- `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB`

### PostgreSQL Tables

Requires PostgreSQL with pgvector extension. Tables are auto-migrated on first use.

**embeddings** - Image embeddings for similarity search
```sql
CREATE TABLE embeddings (
    photo_uid    VARCHAR(255) PRIMARY KEY,
    embedding    vector(768),          -- configurable via EMBEDDING_DIM
    model        VARCHAR(255) NOT NULL,
    pretrained   VARCHAR(255) NOT NULL,
    dim          INTEGER NOT NULL,
    created_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
-- IVFFlat index for cosine similarity search
CREATE INDEX embeddings_vector_idx ON embeddings USING ivfflat (embedding vector_cosine_ops);
```

**faces** - Face embeddings with bounding boxes
```sql
CREATE TABLE faces (
    id           BIGSERIAL PRIMARY KEY,
    photo_uid    VARCHAR(255) NOT NULL,
    face_index   INTEGER NOT NULL,      -- 0-based index for multiple faces per photo
    embedding    vector(512) NOT NULL,  -- fixed 512 dims (buffalo_l/ResNet100)
    bbox         DOUBLE PRECISION[4] NOT NULL,  -- [x1, y1, x2, y2] in pixels
    det_score    DOUBLE PRECISION NOT NULL,     -- detection confidence 0-1
    model        VARCHAR(255) NOT NULL,
    dim          INTEGER NOT NULL DEFAULT 512,
    created_at   TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(photo_uid, face_index)
);
CREATE INDEX faces_photo_uid_idx ON faces(photo_uid);
-- IVFFlat index for cosine similarity search
CREATE INDEX faces_vector_idx ON faces USING ivfflat (embedding vector_cosine_ops);
```

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
  --embedding       Compute image embeddings using llama.cpp (Qwen2.5-VL model)
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

# Include image embeddings (requires llama.cpp server)
go run . photo info --embedding pq8abc123def

# Album with embeddings
go run . photo info --album aq8xyz789ghi --embedding --json
```

**Hash algorithms:**
- **pHash (perceptual hash)**: DCT-based, robust to resizing and minor edits
- **dHash (difference hash)**: Gradient-based, fast computation

Hamming distance ≤ 10 typically indicates near-duplicate images.

**Embeddings:**
- Uses llama.cpp with Qwen2.5-VL-7B-Instruct model
- Requires `LLAMACPP_URL` environment variable (defaults to http://localhost:8080)
- Compare embeddings using cosine similarity (≥0.9 indicates very similar images)

### Photo Faces Command

```bash
go run . photo faces [flags]

Flags:
  --concurrency N   Number of parallel workers (default 5)
  --limit N         Limit number of photos to process (0 = no limit)
```

Detects faces in all photos and stores embeddings in PostgreSQL. Uses the `/embed/face` endpoint which returns:
- Face embeddings (512 dimensions, buffalo_l/ResNet100 model)
- Bounding boxes in pixels [x1, y1, x2, y2]
- Detection scores (0-1 confidence)

The process is resumable - already processed photos are skipped. Photos with no faces are marked as processed (empty entry) to avoid reprocessing.

Examples:
```bash
# Process all photos
go run . photo faces

# Limit to 100 photos with 3 workers
go run . photo faces --limit 100 --concurrency 3
```

### AI Prompts

Located in `internal/ai/prompts/` (embedded at compile time):
- `photo_analysis.txt` - Labels + description only
- `photo_analysis_with_date.txt` - Labels + description + date estimation
- `album_date.txt` - Album-wide date estimation from descriptions

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
