# Photo Sorter

A CLI tool and web interface for organizing photos in [PhotoPrism](https://photoprism.app/) using AI. Automatically analyzes photos, generates labels and descriptions, estimates dates for undated photos, and enables similarity search using image and face embeddings.

> **Note:** This entire project was vibe-coded. Not a single line of code was written by a human - it's 100% AI-generated using [Claude Code](https://claude.ai/code).

## Features

- **AI-Powered Photo Analysis** - Analyze photos and generate labels, descriptions, and date estimates
- **Multiple AI Providers** - Support for OpenAI, Google Gemini, Ollama, and llama.cpp
- **Batch Processing** - Process entire albums with optional batch API for 50% cost savings
- **Image Similarity Search** - Find similar photos using CLIP embeddings
- **Text-to-Image Search** - Search photos by text description with automatic Czech-to-English translation
- **Face Recognition** - Detect faces, find matches across your library, and assign people
- **Face Outlier Detection** - Find incorrectly assigned faces by computing distance from centroid
- **Photo Books** - Create and manage photo book layouts with multiple page formats
- **Era Estimation** - Estimate photo time periods using CLIP embedding comparison
- **Duplicate Detection** - Find near-duplicate photos via embedding similarity
- **Album Suggestions** - Find photos missing from albums via HNSW centroid search
- **Photo Comparison** - Side-by-side photo comparison with metadata diff
- **Slideshow** - Full-screen photo slideshow with keyboard navigation
- **Web Interface** - Browser-based UI with real-time progress updates via SSE
- **Internationalization** - Czech and English language support
- **Dry Run Mode** - Preview changes before applying them

## Requirements

- Go 1.26+
- Node.js 18+ (for web UI)
- A running PhotoPrism instance
- PostgreSQL 15+ with pgvector extension (for vector storage)

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
  -e PHOTOPRISM_URL=http://photoprism:2342 \
  -e PHOTOPRISM_USERNAME=admin \
  -e PHOTOPRISM_PASSWORD=secret \
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
# PhotoPrism connection (required)
PHOTOPRISM_URL=http://localhost:2342
PHOTOPRISM_USERNAME=admin
PHOTOPRISM_PASSWORD=your-password

# Optional: public URL for generating clickable photo links
PHOTOPRISM_DOMAIN=https://photos.example.com

# AI Providers (configure at least one)
OPENAI_TOKEN=sk-...
GEMINI_API_KEY=...

# Local AI (optional)
OLLAMA_URL=http://localhost:11434
OLLAMA_MODEL=llama3.2-vision:11b
LLAMACPP_URL=http://localhost:8080

# Embeddings service (optional)
EMBEDDING_URL=http://localhost:8000
EMBEDDING_DIM=768

# PostgreSQL with pgvector (required)
DATABASE_URL=postgres://user:pass@localhost:5432/photosorter?sslmode=disable
DATABASE_MAX_OPEN_CONNS=25
DATABASE_MAX_IDLE_CONNS=5

# Optional: persist HNSW indexes for fast startup
HNSW_INDEX_PATH=/data/faces.pg.hnsw
HNSW_EMBEDDING_INDEX_PATH=/data/embeddings.pg.hnsw

# Web server (optional)
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

Sync face marker data from PhotoPrism to keep the local cache up-to-date:

```bash
# Sync face markers from PhotoPrism
photo-sorter cache sync

# With custom concurrency
photo-sorter cache sync --concurrency 5

# JSON output for scripting
photo-sorter cache sync --json
```

Push InsightFace embeddings to PhotoPrism's MariaDB:

```bash
# Preview what would be updated
photo-sorter cache push-embeddings --dry-run

# Push embeddings
photo-sorter cache push-embeddings
```

Compute CLIP era embedding centroids for photo era estimation:

```bash
# Compute and save era centroids
photo-sorter cache compute-eras
```

### Web Interface

Start the web server for browser-based access:

```bash
# Production (uses embedded frontend)
photo-sorter serve

# Custom port
photo-sorter serve --port 3000
```

The web UI requires authentication. Log in with your PhotoPrism credentials to access all features.

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
│   ├── config/             # Configuration and pricing
│   ├── constants/          # Shared constants (page sizes, thresholds)
│   ├── database/           # PostgreSQL+pgvector storage backend
│   ├── facematch/          # Face matching utilities (IoU, bbox conversion)
│   ├── fingerprint/        # Perceptual hashing and embeddings
│   ├── photoprism/         # PhotoPrism REST API client
│   ├── sorter/             # Photo analysis orchestration
│   └── web/                # Web server and API handlers
└── web/                    # React + TypeScript frontend
```

### Data Flow

1. CLI command invokes sorter with album UID
2. Sorter fetches photos via PhotoPrism client
3. Each photo is downloaded and sent to AI provider
4. AI suggests categories/labels
5. Labels are applied back to PhotoPrism (unless dry-run)

## Documentation

- [Architecture](docs/architecture.md) - System design, package structure, and data flow
- [CLI Reference](docs/cli-reference.md) - Complete reference for all CLI commands
- [Web UI Guide](docs/web-ui.md) - Guide to the web interface features
- [API Reference](docs/API.md) - REST API documentation
- [HNSW Architecture](docs/hnsw-architecture.md) - In-memory HNSW vs pgvector design rationale
- [Face Markers](docs/markers.md) - Face matching and marker coordinate handling
- [Era Estimation](docs/era-estimation.md) - Era estimation using CLIP embeddings
- [Photo Books](docs/photo-book.md) - Photo book planning tool
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
