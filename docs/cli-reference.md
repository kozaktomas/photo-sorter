# CLI Reference

Complete reference for all Photo Sorter CLI commands.

## Global Flags

| Flag | Description |
|------|-------------|
| `--capture <dir>` | Save API responses to directory for testing |

## Commands

### albums

List albums from the native `albums` table.

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
| `-l, --label` | string[] | none | Labels to apply to uploaded photos (can be specified multiple times) |

**Examples:**
```bash
# Upload from a folder
photo-sorter upload aq8abc123def /path/to/photos

# Upload from multiple folders
photo-sorter upload aq8abc123def /path/folder1 /path/folder2

# Recursive search
photo-sorter upload -r aq8abc123def /path/to/photos

# Upload and apply labels
photo-sorter upload -l "Vacation" -l "Summer" aq8abc123def /path/to/photos
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

**Example:**
```bash
photo-sorter serve --port 3000
```

**Similarity search:**

The server uses pgvector's native HNSW indexes on `embeddings.embedding`
and `faces.embedding` (operator class `vector_cosine_ops`). pgvector
maintains them automatically on INSERT / UPDATE / DELETE — there is no
in-process index, no on-disk file, and no startup or shutdown overhead
beyond opening / closing the DB pool. See
[`docs/similarity-search.md`](similarity-search.md).

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

**Examples:**
```bash
# Single photo
photo-sorter photo info pq8abc123def

# Album with JSON output
photo-sorter photo info --album aq8xyz789 --json
```

---

### Face / embedding diagnostics — web UI only

For deeper face data, use the web UI:

- `GET /api/v1/photos/{uid}/faces` — list detected faces + suggestions
- `POST /api/v1/photos/{uid}/faces/compute` — recompute embeddings
- `POST /api/v1/process/sync-cache` — re-derive cached marker metadata for every face

Face matching and outlier detection are exposed via the web UI and the
equivalent `POST /api/v1/faces/match` / `POST /api/v1/faces/outliers`
endpoints.

---

### cache compute-phashes

Backfill perceptual hashes (pHash + dHash) for photos that lack them in
the `photo_phashes` table. New uploads write a row automatically; this
command fills the gap for photos that pre-date the
`DUPLICATE_PHASH_MAX_DIFF` feature.

```bash
photo-sorter cache compute-phashes [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--limit` | int | 0 | Maximum number of photos to process (0 = unlimited) |
| `--concurrency` | int | 4 | Number of parallel decode + hash workers |
| `--json` | bool | false | Output as JSON instead of a progress bar |

**Examples:**
```bash
# Backfill every photo missing a pHash
photo-sorter cache compute-phashes

# First-run smoke test on the first 1000 photos
photo-sorter cache compute-phashes --limit 1000

# JSON output for scripting
photo-sorter cache compute-phashes --json
```

#### What It Does

1. Selects every `photos.uid` that has no row in `photo_phashes`
2. For each photo (parallelized):
   - Resolves the on-disk primary file via the configured storage tree
   - Runs `imgconvert.EnsureDecodable` so HEIC/RAW originals are funnelled
     through `heif-convert`/`dcraw` into a JPEG-friendly intermediate
   - Computes pHash + dHash via `internal/fingerprint`
   - Upserts the row into `photo_phashes`
3. Reports counts of (hashed, skipped, errored)

#### Prerequisites

- `DATABASE_URL` environment variable must be set
- `STORAGE_ORIGINALS_PATH` / `STORAGE_CACHE_PATH` must point at the
  originals tree the upload pipeline writes to

#### Idempotency

Re-runs are safe — only photos missing a `photo_phashes` row are touched.
To force a re-hash of every photo, truncate the table first:

```sql
TRUNCATE photo_phashes;
```

---

### cache build-thumbs

Generate any missing thumbnails for every photo in the database. Used
after a migration, after a cache wipe, or whenever a new size definition
is added to the registry.

```bash
photo-sorter cache build-thumbs [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--concurrency` | int | 4 | Number of parallel decode + resize workers |
| `--sizes` | strings | (all) | Comma-separated subset of registered sizes (e.g. `fit_720,tile_224`) |
| `--limit` | int | 0 | Maximum number of photos to process (0 = unlimited) |
| `--only-missing` | bool | true | Only generate thumbs that are not already cached. Pass `--only-missing=false` to force regeneration |
| `--photo-uid` | string | "" | Regenerate thumbs for a single photo by UID (overrides `--limit`) |
| `--json` | bool | false | Output as JSON instead of a progress bar |

**Examples:**
```bash
# Backfill every missing thumbnail (default sizes, default concurrency)
photo-sorter cache build-thumbs

# Force-regenerate one photo's thumbs (handy after restoring an original)
photo-sorter cache build-thumbs --photo-uid p123abc --only-missing=false

# Only the sizes the web UI actually serves
photo-sorter cache build-thumbs --sizes fit_720,fit_1920,tile_224

# Smoke-test a fresh migration on the first 50 photos
photo-sorter cache build-thumbs --limit 50
```

#### What It Does

1. Lists photos from the `photos` table (paginated; `--photo-uid` short-circuits to one row).
2. For each photo (parallelised):
   - Resolves the on-disk primary file via the configured storage tree.
   - Runs `imgconvert.EnsureDecodable` so HEIC/RAW originals are funnelled
     through `heif-convert` / `dcraw` into a JPEG-friendly intermediate
     (with the temp file cleaned up via `defer`).
   - Calls `thumb.GenerateSizes` for the requested size subset, which
     decodes the source image once and writes only the missing thumbs.
3. Reports `(photos_scanned, generated, skipped, failed)` — `generated`
   counts individual thumbnail files written, not photos.

Decode failures for a single photo are logged to stderr and counted as
`failed`; the run continues.

#### Prerequisites

- `DATABASE_URL` environment variable must be set.
- `STORAGE_ORIGINALS_PATH` / `STORAGE_CACHE_PATH` must point at the
  originals tree the upload pipeline writes to.

#### Idempotency

With `--only-missing` on (the default), re-runs are safe — photos whose
thumbs are all cached are skipped without rewriting anything.

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


### MCP Server (integrated into serve)

The MCP (Model Context Protocol) server for AI agent integration is part of the `serve` command. When `MCP_API_TOKEN` is set, MCP endpoints are mounted at `/mcp/sse` and `/mcp/message` on the same HTTP server. If the token is not set, MCP routes are not registered.

**Example:**
```bash
export MCP_API_TOKEN=my-secret-token
photo-sorter serve --port 8085
# MCP available at http://localhost:8085/mcp/sse
# Web UI available at http://localhost:8085/
```

MCP clients authenticate with `Authorization: Bearer <MCP_API_TOKEN>`.

**Available Tools (48 total):**
- **Books** (5): `list_books`, `get_book`, `create_book`, `update_book`, `delete_book`
- **Chapters** (4): `create_chapter`, `update_chapter`, `delete_chapter`, `reorder_chapters`
- **Sections** (8): `create_section`, `update_section`, `delete_section`, `reorder_sections`, `list_section_photos`, `add_photos_to_section`, `remove_photos_from_section`, `update_section_photo`
- **Pages & Slots** (9): `create_page`, `update_page`, `delete_page`, `reorder_pages`, `assign_photo_to_slot`, `assign_text_to_slot`, `clear_slot`, `swap_slots`, `update_slot_crop`
- **Photos** (7): `list_photos`, `get_photo`, `get_photo_thumbnail`, `update_photo`, `get_photo_faces`, `find_similar_photos`, `search_photos_by_text`
- **Albums** (6): `list_albums`, `get_album`, `create_album`, `get_album_photos`, `add_photos_to_album`, `remove_photos_from_album`
- **Labels** (6): `list_labels`, `get_label`, `update_label`, `delete_labels`, `add_photo_label`, `remove_photo_label`
- **Text & AI** (5): `check_text`, `rewrite_text`, `check_consistency`, `list_text_versions`, `restore_text_version`

See [API Reference — MCP Server](API.md#mcp-server) for detailed parameter documentation.

---

### backup

Create a timestamped backup of the originals directory and the photo-sorter Postgres database. The thumbnail cache is intentionally excluded because it can be regenerated from the originals via the thumbnail backfill job.

```bash
photo-sorter backup --output <dir> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--output` | string | (required) | Directory where backups are written |
| `--originals-path` | string | `$STORAGE_ORIGINALS_PATH` | Path to the originals tree |
| `--db-url` | string | `$DATABASE_URL` | Postgres connection URL passed to `pg_dump` |
| `--keep` | int | 14 | Number of backups to retain (0 disables pruning) |
| `--compress` | string | zstd | Compression algorithm: `zstd` or `gzip` |
| `--skip-originals` | bool | false | Skip the originals tar |
| `--skip-db` | bool | false | Skip the `pg_dump` |
| `--cleanup-on-failure` | bool | false | Remove the `.tmp` directory if the run fails |
| `--progress-every` | int | 500 | Originals progress cadence (files between log lines) |

**Output layout:** Each run produces `<output>/photo-sorter-<YYYYMMDD-HHMMSS>/` containing three sibling files so they can be restored selectively:

- `metadata.json` — `{ created_at, sorter_version, db_size_bytes, originals_bytes, file_count }`
- `db.sql.zst` (or `.gz`) — `pg_dump --format=plain --no-owner --no-privileges` piped through the compressor.
- `originals.tar.zst` (or `.tar.gz`) — streamed tar of the originals directory, preserving relative paths.

The directory is written first to `.photo-sorter-<ts>.tmp/` and only renamed atomically once both artifacts succeed; failed runs leave the `.tmp` directory in place unless `--cleanup-on-failure` is set.

**Requirements:**

- `pg_dump` must be on PATH (`apt: postgresql-client`; the Docker image already ships it).

**Examples:**

```bash
# Daily backup keeping the last 14 runs.
photo-sorter backup --output /var/backups/photo-sorter --keep 14

# Originals only (no database access).
photo-sorter backup --output /tmp/bak --skip-db

# Gzip instead of zstd, retain only 3 runs.
photo-sorter backup --output /tmp/bak --compress gzip --keep 3
```

#### Scheduling with systemd

Sample units live in [`deploy/systemd/`](../deploy/systemd/). Install with:

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

The timer triggers `photo-sorter-backup.service` every day at 03:00 (with a 10-minute random jitter and persistence across reboots).

---

### DB Commands

The `db-export` / `db-import` pair covers the metadata side of disaster
recovery: embeddings, faces, books, users, sessions, era_embeddings,
photos, albums, labels, markers, subjects — everything that lives in the
`photosorter` database. Photos themselves (the originals tree on disk)
are NOT part of these commands; back those up separately via
rsync/borg/etc. or the higher-level `backup` command above.

Both commands read `DATABASE_URL` from the environment and shell out to
the system `pg_dump` / `pg_restore` / `psql` binaries (install
`postgresql-client`).

#### db-export

Dump the photo-sorter PostgreSQL database to a single file.

```bash
photo-sorter db-export [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--output`, `-o` | string | `photosorter-<UTC timestamp>.<ext>` | Destination file path. Extension matches the format. |
| `--format` | string | `custom` | Dump format: `custom` (pg_dump's compressed binary) or `plain` (SQL). |
| `--no-compress` | bool | false | For `plain` format, skip gzipping the output. Ignored for `custom` (already compressed). |
| `--force` | bool | false | Overwrite the output file if it already exists. |

The `custom` format is recommended: it is what `pg_restore` expects, and
it supports selective restores. Use `plain` only if you need a
human-readable SQL file.

On success the command prints the output path, final size, and elapsed
wall time. If `pg_dump` exits non-zero, the partial output file is
removed automatically.

**Examples:**

```bash
# Default: custom-format dump to the current directory with an auto-timestamped name.
photo-sorter db-export

# Plain SQL, gzipped, named explicitly.
photo-sorter db-export --format plain -o sorter.sql.gz

# Uncompressed plain SQL (useful for diffing).
photo-sorter db-export --format plain --no-compress -o sorter.sql
```

#### db-import

Restore a photo-sorter PostgreSQL database from a dump file produced by
`db-export` (or any equivalent `pg_dump` output).

```bash
photo-sorter db-import [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--input`, `-i` | string | (required) | Source dump file. |
| `--yes`, `-y` | bool | false | Skip the interactive confirmation prompt. |
| `--drop-existing` | bool | false | `DROP SCHEMA public CASCADE; CREATE SCHEMA public;` before restoring (clean slate). |

The dump format is auto-detected from the file header — `PGDMP` magic
bytes for `pg_restore`, anything that looks like SQL for `psql`. Gzipped
files are decompressed transparently. For `custom` format dumps that are
also gzipped, the import gunzips to a temp file first (pg_restore needs
a seekable file) and removes it afterward.

If the target database already contains data (the `embeddings` table has
rows), `db-import` refuses to overwrite it without `--yes`. The
interactive prompt requires the literal token `yes`.

For `custom` format dumps, `db-import` invokes `pg_restore --clean
--if-exists` unless `--drop-existing` is set; for `plain` format dumps,
the SQL statements emitted by `pg_dump` (or by `db-export --format
plain`) handle the drop/create ordering themselves.

**Examples:**

```bash
# Restore a custom-format dump (auto-detected).
photo-sorter db-import -i sorter.dump --yes

# Restore a plain SQL gzipped dump.
photo-sorter db-import -i sorter.sql.gz --yes

# Force a clean slate first (drops public schema).
photo-sorter db-import -i sorter.dump --yes --drop-existing
```

**Next steps after a successful import:**

1. Restart the photo-sorter server (HNSW indexes load at startup).
2. If the imported DB came from a host with a different originals tree,
   verify `STORAGE_ORIGINALS_PATH` and re-run `photo-sorter cache
   build-thumbs` to regenerate thumbnails for any missing sizes.
3. If face-search results look off, hit `POST
   /api/v1/process/rebuild-index` (Rebuild Index button in the UI) to
   rebuild the in-memory HNSW indexes from the freshly imported
   embeddings.

---

### migrate-from-photoprism

One-shot import of an existing PhotoPrism instance into photo-sorter's
native PostgreSQL schema and storage tree. Reads directly from
PhotoPrism's MariaDB and copies primary originals from the source
filesystem.

```bash
photo-sorter migrate-from-photoprism \
  --pp-db "<DSN>" \
  --pp-originals <path> \
  [--pp-cache <path>] \
  [--uploader-username <name>] \
  [--dry-run] [--skip-thumbs] \
  [--batch-size 200] [--concurrency 4] \
  [--only subjects,photos,labels,albums,markers,thumbs] \
  [--emit-photo-map /tmp/photo-map.json]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--pp-db` | string | (required) | PhotoPrism MariaDB DSN (`user:pw@tcp(host:3306)/db`) |
| `--pp-originals` | string | (required) | Path to PhotoPrism's originals directory |
| `--pp-cache` | string | `""` | PhotoPrism storage/cache dir (currently unused; reserved) |
| `--uploader-username` | string | `""` | Native photo-sorter username written to `photos.uploaded_by` |
| `--dry-run` | bool | false | Walk PhotoPrism and print counts; no DB writes, no file copies |
| `--skip-thumbs` | bool | false | Skip thumbnail regeneration |
| `--batch-size` | int | 200 | Source DB query batch size |
| `--concurrency` | int | 4 | Thumbnail generation worker count |
| `--only` | strings | (all) | Limit stages: `subjects`, `photos`, `labels`, `albums`, `markers`, `thumbs` |
| `--emit-photo-map` | string | `""` | Optional path: after the photos stage, write a JSON dump of the PhotoPrism→native photo UID map (consumed by `migrate-remap-references`). Identity map in the happy path. |

**UID preservation:** PhotoPrism UIDs for photos, albums, subjects, and
markers are written verbatim into the native `uid` columns. Cached
PhotoPrism references in `embeddings`, `faces` (`subject_uid`,
`marker_uid`), `section_photos`, and `page_slots` therefore reconnect
without a remap pass. Operators who already ran an older buggy version
of this command (which generated new UIDs) should follow the migration
with `migrate-remap-references --map <emit-file>`.

**Stages (in order):**

1. **subjects** — read PhotoPrism `subjects` and upsert into native `subjects` (idempotent on name).
2. **photos** — read `photos` joined with `files`/`cameras`/`lenses`/`details`; SHA256-hash each primary file, skip if `photos.file_hash` already exists, copy bytes into `STORAGE_ORIGINALS_PATH/YYYY/MM/<basename>`, insert the photo row, attach non-primary files.
3. **labels** — upsert labels by slug (priority < 0 excluded), then attach `photos_labels` rows with `source = "import"`.
4. **albums** — upsert albums by slug, then attach `photos_albums` rows (skipping already-linked photos).
5. **markers** — create face/label markers for newly-created photos. Markers are skipped for photos that already existed before this run (they are presumed to already carry their markers).
6. **thumbnails** — regenerate every cached thumbnail size for the photos created in this run (skipped via `--skip-thumbs`).

**Idempotency:** photos are skipped by SHA256 file_hash; subjects/labels/albums are looked up by name/slug before insert; album_photos and photo_labels are pre-checked; markers are only created for newly-inserted photos. A re-run prints "Created=0" for every stage.

**Examples:**

```bash
# Dry-run against a live PhotoPrism (find missing files, estimate scope).
photo-sorter migrate-from-photoprism \
  --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
  --pp-originals /photoprism/originals \
  --uploader-username admin \
  --dry-run

# Full migration without regenerating thumbnails (run cache compute-*
# afterwards instead).
photo-sorter migrate-from-photoprism \
  --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
  --pp-originals /photoprism/originals \
  --uploader-username admin \
  --skip-thumbs

# Re-run only the markers stage (after manual cleanup, for example).
photo-sorter migrate-from-photoprism \
  --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
  --pp-originals /photoprism/originals \
  --only markers
```

The native uploader user must exist before the migration runs;
create one with the admin tooling (or leave `--uploader-username` empty
to write NULL `uploaded_by`).

---

### migrate-verify

Compare an existing PhotoPrism instance against the photo-sorter native
database after a migration. Runs read-only against both data sources
and prints (or emits as JSON) a two-phase diff:

1. **Structural** — counts, existence (`missing_in_sorter` /
   `orphan_in_sorter`), per-album/label photo memberships, marker
   geometry drift, on-disk orphan files.
2. **Field-level** — every column the migrator is supposed to copy is
   compared cell-by-cell. A field-level mismatch is reported as
   `field_diffs[]` per entity (JSON) and as colour-coded lines below
   the structural section (text). Tolerance bands swallow 1-second
   drift on dates, 1e-6 on lat/lng, 1 m on altitude, and 0.01 on
   marker score by default; use `--strict` to treat them as diffs.

Zero diffs from `migrate-verify` is the authoritative gate for
cancelling the PhotoPrism + MariaDB compose services.

```bash
photo-sorter migrate-verify \
  --pp-db "<DSN>" \
  --pp-originals <path> \
  [--json] [--no-color] [--concurrency <N>] [--fields-only] [--strict]
```

Flags:

| Flag             | Description                                                                          |
|------------------|--------------------------------------------------------------------------------------|
| `--pp-db`        | PhotoPrism MariaDB DSN, e.g. `photoprism:photoprism@tcp(mariadb:3306)/photoprism`.   |
| `--pp-originals` | Path to the PhotoPrism originals directory (the verifier rehashes primary files).    |
| `--json`         | Emit a machine-readable JSON report instead of the human-readable text.              |
| `--no-color`     | Disable ANSI colour escapes in the human-readable report (useful for log files/CI). |
| `--concurrency`  | Goroutine-pool size for the SHA256 re-hash pass (default 4).                         |
| `--fields-only`  | Skip the structural existence/disk pass and run only the field-level diff. Useful when iterating on migrator fixes (the rehash pass dominates wall time on large libraries). |
| `--strict`       | Drop every tolerance band: 1-second drift on `taken_at`, 1e-6 on lat/lng, 1 m on altitude, 0.01 on marker score all become diffs. |

Sections in the report:

1. **photos** — PhotoPrism row count vs sorter `photos` count. Each PP
   primary file is re-hashed with SHA256 and looked up by `file_hash`;
   misses go into `missing_in_sorter`. The reverse pass flags sorter
   rows whose hash has no PhotoPrism counterpart as `orphan_in_sorter`.
   Field diff covers `taken_at` / `taken_at_source` / `time_zone` /
   `taken_at_offset`, `description` / `notes`, `keywords` (sorted +
   NFC-normalised), GPS (`lat` / `lng` / `altitude`), camera /
   lens (`camera_make` / `camera_model` / `lens_model`), exposure
   (`iso` / `f_number` / `exposure` / `focal_length`), dimensions
   (`width` / `height` / `orientation`), flags (`favorite` / `private`
   / `panorama` / `scan` / `quality`), and EXIF text (`exif_artist` /
   `exif_copyright` / `exif_license` / `exif_software`).
2. **albums** — slug + title parity, per-album symmetric photo diff,
   plus a widened per-pair `membership_diffs` list (PhotoPrism photo
   `file_hash[:8]` + album slug for each asymmetric membership). Field
   diff covers `description`, `location`, `category`, `notes`,
   `filter`, `album_order`, `favorite`, `private`, `type`.
3. **labels** — slug + name parity, per-label photo-pair counts, and
   per-pair `membership_diffs` (same shape as albums). Field diff
   covers `description`, `categories`, `priority`, `favorite`.
4. **subjects / markers** — accent-insensitive subject name match,
   per-subject marker count diffs, and per-marker geometry drift
   (markers whose x/y/w/h differs by more than 1% on any axis). Subject
   field diff covers `bio`, `about`, `alias`, `favorite`, `private`,
   `type`. Marker field diff covers `score` (≤ 0.01 tolerance),
   `invalid`, `reviewed`, and `subject_uid` linkage.
5. **disk** — orphan files: every regular file under the sorter's
   originals root that has no corresponding `photos.file_path` row.

JSON output: each entity's `field_diffs[]` is capped at 1000 entries
per field name (a single noisy field cannot crowd out diffs from
others). The human-readable output samples the first 50 entries per
entity and prints a `...and N more truncated (use --json for the full
report)` footer when the bucket overflowed.

Examples:

```bash
# Human-readable report with ANSI colour.
photo-sorter migrate-verify \
  --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
  --pp-originals /photoprism/originals

# Skip the slow rehash pass and run only the field-level diff.
photo-sorter migrate-verify \
  --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
  --pp-originals /photoprism/originals \
  --fields-only

# Strict mode: catch even sub-second taken_at drift.
photo-sorter migrate-verify \
  --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
  --pp-originals /photoprism/originals \
  --strict

# Machine-readable JSON, for jq/CI integration.
photo-sorter migrate-verify \
  --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
  --pp-originals /photoprism/originals \
  --json > verify.json
```

Exit code is `0` when no differences are found and `1` when at least one
diff is reported, so the command can be chained into automated
post-migration checks.

---

### migrate-remap-references

Rewrite every soft `photo_uid` reference in the native database using a
photo-UID map (as emitted by `migrate-from-photoprism --emit-photo-map`).

Intended for operators who landed an older buggy version of the migrator
that wrote generated UIDs into `photos` instead of preserving the
PhotoPrism UIDs. After the fixed migrator runs, the operator's
historical `embeddings` / `faces` / `section_photos` / `page_slots`
rows still reference the old (buggy) UIDs; this command rewrites them
in one transaction so they point at the new UIDs.

```bash
photo-sorter migrate-remap-references \
  --map <path> \
  [--dry-run] [--yes]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--map` | string | (required) | Path to the photo-UID map JSON file (`{"version":1,"photo_uid_map":{...},"file_uid_map":{...}}`) |
| `--dry-run` | bool | false | Run the UPDATEs and roll back; report row counts and orphan stats without writing |
| `--yes` | bool | false | Skip the interactive confirmation prompt |

**Tables rewritten** (every (old, new) pair in `photo_uid_map`):

- `embeddings.photo_uid`
- `faces.photo_uid`
- `faces_processed.photo_uid`
- `markers.photo_uid`
- `album_photos.photo_uid`
- `photo_labels.photo_uid`
- `photo_phashes.photo_uid`
- `section_photos.photo_uid`
- `page_slots.photo_uid`

All updates run inside one Postgres transaction; either every table is
remapped or nothing is. An identity map (every key equal to its value)
short-circuits before any work is done — the command prints
"nothing to remap" and exits 0.

After the UPDATEs, the command runs an integrity audit and prints, per
table, how many rows now point at a `photo_uid` that does not match any
`photos.uid`. Non-zero is informational (some PhotoPrism originals may
have been deleted before the migration); the command does not fail.

**Examples:**

```bash
# Dry run against the file the migrator just wrote.
photo-sorter migrate-remap-references --map /tmp/photo-map.json --dry-run

# Apply remap, skip the confirmation.
photo-sorter migrate-remap-references --map /tmp/photo-map.json --yes
```

See [`docs/specs/cross-server-migration.md`](specs/cross-server-migration.md)
for the full migration runbook.

---

### version

Print the version number.

```bash
photo-sorter version
```
