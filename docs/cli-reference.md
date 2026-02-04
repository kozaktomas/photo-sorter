# CLI Reference

Complete reference for all Photo Sorter CLI commands.

## Global Flags

| Flag | Description |
|------|-------------|
| `--capture <dir>` | Save API responses to directory for testing |

## Commands

### albums

List all albums from PhotoPrism.

```bash
photo-sorter albums [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--count` | int | 100 | Number of albums to retrieve |
| `--offset` | int | 0 | Offset for pagination |
| `--order` | string | | Sort order (e.g., 'name', 'count') |
| `--query` | string | | Search query to filter albums |

**Example:**
```bash
photo-sorter albums --count 50 --order name
```

---

### sort

Analyze photos in an album using AI and apply labels, descriptions, and date estimates.

```bash
photo-sorter sort <album-uid> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | bool | false | Preview changes without applying them |
| `--limit` | int | 0 | Limit number of photos to process (0 = no limit) |
| `--individual-dates` | bool | false | Estimate date per photo instead of album-wide |
| `--batch` | bool | false | Use batch API for 50% cost savings (slower) |
| `--provider` | string | openai | AI provider: openai, gemini, ollama, llamacpp |
| `--force-date` | bool | false | Overwrite existing dates with AI estimates |
| `--concurrency` | int | 5 | Number of parallel requests |

**Examples:**
```bash
# Preview changes
photo-sorter sort aq8abc123def --dry-run

# Use Gemini with individual dates
photo-sorter sort aq8abc123def --provider gemini --individual-dates

# Batch mode for cost savings
photo-sorter sort aq8abc123def --batch

# High concurrency
photo-sorter sort aq8abc123def --concurrency 10
```

---

### labels

List and manage labels.

```bash
photo-sorter labels [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--count` | int | 1000 | Maximum number of labels to retrieve |
| `--all` | bool | true | Include all labels (including unused) |
| `--sort` | string | name | Sort order: name, -name, count, -count |
| `--min-photos` | int | 0 | Only show labels with at least N photos |

**Examples:**
```bash
# List all labels
photo-sorter labels

# Sort by photo count (descending)
photo-sorter labels --sort=-count

# Only labels with 5+ photos
photo-sorter labels --min-photos=5
```

#### labels delete

Delete labels by UID.

```bash
photo-sorter labels delete <uid1> [uid2...] [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--yes` | bool | false | Skip confirmation prompt |

**Example:**
```bash
photo-sorter labels delete lq8abc123 lq8def456 --yes
```

---

### count

Count photos in an album.

```bash
photo-sorter count <album-uid>
```

---

### create

Create a new album.

```bash
photo-sorter create <album-name>
```

**Example:**
```bash
photo-sorter create "Summer Vacation 2024"
```

---

### clear

Remove all photos from an album (keeps photos in library).

```bash
photo-sorter clear <album-uid> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--yes` | bool | false | Skip confirmation prompt |

**Example:**
```bash
photo-sorter clear aq8abc123def --yes
```

---

### move

Move all photos from source album to a newly created album.

```bash
photo-sorter move <source-album-uid> <new-album-name>
```

**Example:**
```bash
photo-sorter move aq8abc123def "Sorted Photos 2024"
```

---

### upload

Upload photos to an album.

```bash
photo-sorter upload <album-uid> <folder-path> [folder-path...] [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-r, --recursive` | bool | false | Search for photos recursively in subdirectories |

**Examples:**
```bash
# Upload from a folder
photo-sorter upload aq8abc123def /path/to/photos

# Upload from multiple folders
photo-sorter upload aq8abc123def /path/folder1 /path/folder2

# Recursive search
photo-sorter upload -r aq8abc123def /path/to/photos
```

---

### serve

Start the web server.

```bash
photo-sorter serve [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--port` | int | 8080 | Server port |
| `--host` | string | 0.0.0.0 | Server host |
| `--session-secret` | string | (random) | Secret for signing session cookies |

**Environment Variables:**

| Variable | Description |
|----------|-------------|
| `WEB_PORT` | Override `--port` flag |
| `WEB_HOST` | Override `--host` flag |
| `WEB_SESSION_SECRET` | Override `--session-secret` flag |
| `HNSW_INDEX_PATH` | Path to persist face HNSW index for PostgreSQL backend (enables fast startup) |
| `HNSW_EMBEDDING_INDEX_PATH` | Path to persist embedding HNSW index for PostgreSQL backend (enables fast startup) |

**Example:**
```bash
photo-sorter serve --port 3000
```

**PostgreSQL Backend with HNSW Persistence:**

When using PostgreSQL backend (`DATABASE_URL` set), the server builds an in-memory HNSW index at startup for fast face similarity search. By default, this takes ~4 minutes for 45k faces and must be repeated on every restart.

To enable fast startup, set `HNSW_INDEX_PATH` to persist the index to disk:

```bash
export HNSW_INDEX_PATH=/data/faces.pg.hnsw
photo-sorter serve
```

The index is:
- Saved on graceful shutdown (Ctrl+C)
- Saved after "Rebuild Index" operations
- Loaded from disk on startup if fresh (matching face count and max ID)
- Rebuilt from database if stale or missing

---

### photo info

Get perceptual hashes and metadata for photos.

```bash
photo-sorter photo info <photo-uid> [flags]
photo-sorter photo info --album <album-uid> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--album` | string | | Process all photos in an album |
| `--json` | bool | false | Output as JSON |
| `--limit` | int | 0 | Limit number of photos in album mode |
| `--concurrency` | int | 5 | Number of parallel workers |
| `--embedding` | bool | false | Compute image embeddings |

**Examples:**
```bash
# Single photo
photo-sorter photo info pq8abc123def

# Album with JSON output
photo-sorter photo info --album aq8xyz789 --json

# With embeddings
photo-sorter photo info --embedding pq8abc123def
```

---

### photo clear-faces

Remove all face markers from a photo in PhotoPrism.

```bash
photo-sorter photo clear-faces <photo-uid> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | bool | false | Preview changes without applying them |
| `--assigned-only` | bool | false | Only remove markers with person assignments |

**Examples:**
```bash
# Delete all face markers
photo-sorter photo clear-faces pt4abc123def

# Only remove assigned markers
photo-sorter photo clear-faces pt4abc123def --assigned-only

# Preview changes
photo-sorter photo clear-faces pt4abc123def --dry-run
```

---

### photo similar

Find visually similar photos using image embeddings.

```bash
photo-sorter photo similar [photo-uid] [flags]
photo-sorter photo similar --label <label-name> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--threshold` | float | 0.3 | Maximum cosine distance (lower = more similar) |
| `--limit` | int | 50 | Maximum number of results |
| `--json` | bool | false | Output as JSON |
| `--label` | string[] | | Find photos similar to all photos with this label |
| `--apply` | bool | false | Apply the label(s) to similar photos found |
| `--dry-run` | bool | false | Preview label assignments without applying |

**Examples:**
```bash
# Find similar to a photo
photo-sorter photo similar pq8abc123def

# Find similar by label
photo-sorter photo similar --label "cat"

# Multiple labels
photo-sorter photo similar --label "cat" --label "dog"

# Apply labels to matches
photo-sorter photo similar --label "cat" --apply

# Preview first
photo-sorter photo similar --label "cat" --apply --dry-run
```

---

### photo match

Find all photos containing a specific person by comparing face embeddings.

```bash
photo-sorter photo match <person-name> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--threshold` | float | 0.5 | Maximum cosine distance for face matching |
| `--limit` | int | 0 | Limit number of results (0 = no limit) |
| `--json` | bool | false | Output as JSON |
| `--apply` | bool | false | Apply changes to PhotoPrism (create markers and assign person) |
| `--dry-run` | bool | false | Preview changes without applying them (use with --apply) |

**Examples:**
```bash
# Find photos of a person
photo-sorter photo match john-doe

# Strict matching
photo-sorter photo match john-doe --threshold 0.3

# Preview changes
photo-sorter photo match john-doe --apply --dry-run

# Apply changes
photo-sorter photo match john-doe --apply
```

#### How It Works

1. Queries PhotoPrism for photos tagged with the person
2. Retrieves face embeddings for those photos from PostgreSQL
3. Uses cosine distance to find similar faces across all photos
4. Only keeps faces that match at least 10% of the source embeddings (reduces false positives)
5. Compares bounding boxes (IoU) with existing PhotoPrism markers to determine action

#### Threshold Guidelines

| Threshold | Behavior | Use Case |
|-----------|----------|----------|
| 0.2 - 0.3 | Very strict | High confidence matches only |
| 0.3 - 0.4 | Strict | Good for well-lit photos with clear faces |
| 0.4 - 0.5 | Moderate (default) | General use |
| 0.5 - 0.6 | Loose | Catches more but increases false positives |

Face embeddings vary due to lighting, pose angle, image quality, age, occlusions, and expression.

#### Output Actions

| Action | Description |
|--------|-------------|
| `create_marker` | No marker exists - need to create one |
| `assign_person` | Marker exists but no person assigned |
| `already_done` | Already correctly tagged |

#### Prerequisites

1. Use the web UI Process page to detect faces and store embeddings
2. At least some photos must be tagged with the person name in PhotoPrism
3. `DATABASE_URL` environment variable must be set

---

### cache sync

Sync face marker data from PhotoPrism to the local PostgreSQL cache.

```bash
photo-sorter cache sync [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--concurrency` | int | 20 | Number of parallel workers |
| `--json` | bool | false | Output as JSON instead of progress bar |

**Examples:**
```bash
# Run sync with default concurrency
photo-sorter cache sync

# Limit concurrency
photo-sorter cache sync --concurrency 5

# JSON output for scripting
photo-sorter cache sync --json
```

#### When to Use

Use `cache sync` when faces have been assigned or unassigned directly in PhotoPrism's native UI. The local cache stores marker assignments to avoid repeated API calls during face matching. This command refreshes the cache to match PhotoPrism's current state.

#### What It Does

1. Gets all photos with detected faces from the database
2. For each photo (parallelized):
   - Fetches current photo details from PhotoPrism
   - Detects deleted photos: hard-deleted (404) or soft-deleted (`DeletedAt` field set) — removes their faces, embeddings, and processing records
   - Fetches current markers from PhotoPrism
   - Matches database faces to markers using IoU
   - Updates cached marker UID, subject UID, and subject name
   - Updates cached photo dimensions and orientation
3. Reports how many faces were updated and photos cleaned up

#### Prerequisites

- `DATABASE_URL` environment variable must be set
- PhotoPrism credentials configured
- Faces already detected via web UI Process page

---

### cache compute-eras

Compute CLIP text embedding centroids for photo era estimation.

```bash
photo-sorter cache compute-eras [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | bool | false | Compute embeddings but don't save to database |
| `--json` | bool | false | Output as JSON |

**Examples:**
```bash
# Preview without saving
photo-sorter cache compute-eras --dry-run

# Compute and save era embeddings
photo-sorter cache compute-eras

# JSON output
photo-sorter cache compute-eras --json
```

#### What It Does

1. For each of 16 eras (1900s through 2025-2029), generates 20 text prompts describing typical visual characteristics of photos from that era
2. Computes CLIP text embeddings for each prompt via the embedding service (`POST /embed/text`)
3. Averages the 20 embeddings into a single L2-normalized centroid per era
4. Stores the centroids in the `era_embeddings` PostgreSQL table

The resulting centroids can be compared against photo image embeddings using cosine distance to estimate which era a photo belongs to.

#### Prerequisites

- `DATABASE_URL` environment variable must be set
- `EMBEDDING_URL` environment variable must be set (or defaults to `http://localhost:8000`)

---

### cache push-embeddings

Push InsightFace embeddings from PostgreSQL to PhotoPrism's MariaDB, replacing the default TensorFlow embeddings.

```bash
photo-sorter cache push-embeddings [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | bool | false | Preview changes without writing to MariaDB |
| `--recompute-centroids` | bool | false | Recompute face cluster centroids from new embeddings |
| `--json` | bool | false | Output as JSON |

**Examples:**
```bash
# Preview changes
photo-sorter cache push-embeddings --dry-run

# Push embeddings
photo-sorter cache push-embeddings

# Push and recompute centroids
photo-sorter cache push-embeddings --recompute-centroids

# JSON output
photo-sorter cache push-embeddings --json
```

#### What It Does

1. Fetches all faces from PostgreSQL that have a `marker_uid` linkage
2. For each face, writes its InsightFace embedding to MariaDB `markers.embeddings_json` as `[[e1,...,e512]]`
3. If `--recompute-centroids`: for each face cluster, averages all linked marker embeddings and writes the centroid to `faces.embedding_json`

#### Prerequisites

- `DATABASE_URL` environment variable must be set (PostgreSQL)
- `PHOTOPRISM_DATABASE_URL` environment variable must be set (MariaDB DSN, e.g., `photoprism:photoprism@tcp(mariadb:3306)/photoprism`)
- Faces already detected and linked to markers (run `cache sync` first)

---

### version

Print the version number.

```bash
photo-sorter version
```
