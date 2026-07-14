# CLI Reference

Complete reference for every `photo-sorter` Cobra command and flag, as
registered by the binary today. Where a command's behaviour overlaps
the REST surface, the relevant entry in [`docs/API.md`](API.md) is
cross-referenced; for backup and disaster recovery see
[`docs/backup.md`](backup.md).

> **External binaries required by the upload + EXIF pipelines.** The
> `serve` and `migrate-from-photoprism` commands shell out to
> `exiftool`, `heif-convert` (libheif-tools), and `dcraw` (LibRaw shim)
> when they need to read RAW/HEIC pixels or write XMP sidecars. The
> official Docker image bundles all three; for local development install
> them via the OS package manager (`apt install exiftool libheif-examples
> libraw-bin` on Debian/Ubuntu). `serve` logs a startup WARN line for
> each missing binary so deployments fail loud, not silent.

> **PhotoPrism vs native commands.** Photo-sorter's day-to-day surface is
> the REST API exposed by `serve` (and the embedded web UI). Most CLI
> commands listed here predate the native pipeline and still target a
> legacy PhotoPrism MariaDB instance (`PHOTOPRISM_URL` /
> `PHOTOPRISM_USERNAME` / `PHOTOPRISM_PASSWORD`). They are kept for
> emergency reruns and one-shot migrations; the commands explicitly
> flagged "legacy" below have no native equivalent and only make sense
> against an existing PhotoPrism install.

## Global Flags

| Flag | Description |
|------|-------------|
| `--capture <dir>` | Save PhotoPrism API responses to a directory (test/debug aid). |

## Commands

### albums

List albums from the PhotoPrism REST API. *Legacy* — the native
equivalent is `GET /api/v1/albums` (see [API §Albums](API.md#albums)).

```bash
photo-sorter albums [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--count` | int | 100 | Number of albums to retrieve |
| `--offset` | int | 0 | Offset for pagination |
| `--order` | string | `""` | Sort order (e.g., `name`, `count`) |
| `--query` | string | `""` | Search query to filter albums |

**Example:**
```bash
photo-sorter albums --count 50 --order name
```

---

### sort

Analyze photos in a PhotoPrism album using AI and apply labels,
descriptions, and date estimates. Labels are written via the native
`labels` PostgreSQL repository; album/photo metadata is still fetched
through PhotoPrism, so `PHOTOPRISM_URL` is required.

```bash
photo-sorter sort <album-uid> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | bool | `false` | Preview changes without applying them |
| `--limit` | int | `0` | Limit number of photos to process (`0` = no limit) |
| `--individual-dates` | bool | `false` | Estimate a date per photo instead of one date for the whole album |
| `--batch` | bool | `false` | Use the batch API for 50% cost savings (slower; may take minutes) |
| `--provider` | string | `openai` | AI provider: `openai`, `gemini`, `ollama`, `llamacpp` |
| `--force-date` | bool | `false` | Overwrite existing dates with AI estimates. By default `TakenAt` is only set when the photo currently has no date (`Year` 0/1). |
| `--concurrency` | int | `5` | Number of parallel requests in standard mode |

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

REST equivalent: `POST /api/v1/sort` (see [API §Sort](API.md#sort-ai-analysis)).

---

### labels

List labels from PhotoPrism. *Legacy* — the native equivalent is
`GET /api/v1/labels`.

```bash
photo-sorter labels [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--count` | int | `1000` | Maximum number of labels to retrieve |
| `--all` | bool | `true` | Include all labels (even unused ones) |
| `--sort` | string | `name` | Sort by: `name`, `count`, `-name`, `-count` (prefix `-` for descending) |
| `--min-photos` | int | `0` | Only show labels with at least N photos |

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

Delete labels by UID. Unknown UIDs are skipped with a warning and the
final summary counts only UIDs that actually existed.

```bash
photo-sorter labels delete <uid1> [uid2...] [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--yes` | bool | `false` | Skip the confirmation prompt |

**Example:**
```bash
photo-sorter labels delete lq8abc123 lq8def456 --yes
```

---

### count

Count photos in a PhotoPrism album. *Legacy.*

```bash
photo-sorter count <album-uid>
```

---

### create

Create a new album in PhotoPrism. *Legacy PhotoPrism-only tool* — the
native pipeline does not use it; create albums via `POST /api/v1/albums`
instead.

```bash
photo-sorter create <album-name>
```

**Example:**
```bash
photo-sorter create "Summer Vacation 2024"
```

---

### clear

Remove all photos from a PhotoPrism album (keeps the photos in the
library). *Legacy PhotoPrism-only tool* — not used by the native
pipeline.

```bash
photo-sorter clear <album-uid> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--yes` | bool | `false` | Skip the confirmation prompt |

**Example:**
```bash
photo-sorter clear aq8abc123def --yes
```

---

### move

Move all photos from a source PhotoPrism album to a newly created
album. *Legacy.*

```bash
photo-sorter move <source-album-uid> <new-album-name>
```

**Example:**
```bash
photo-sorter move aq8abc123def "Sorted Photos 2024"
```

---

### upload

Upload photos to a PhotoPrism album. *Legacy* — the native equivalent
is `POST /api/v1/upload` (multipart) or `POST /api/v1/upload/job`
(background job with SSE progress). Supported extensions: `jpg`, `jpeg`,
`png`, `gif`, `heic`, `heif`, `webp`, `tiff`, `tif`, `bmp`, `raw`,
`cr2`, `nef`, `arw`, `dng`.

```bash
photo-sorter upload <album-uid> <folder-path> [folder-path...] [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-r, --recursive` | bool | `false` | Search for photos recursively in subdirectories |
| `-l, --label` | string[] | `[]` | Labels to apply to uploaded photos (repeatable) |

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

Start the photo-sorter web server. The HTTP server hosts the REST API
plus the embedded React SPA, and — when `MCP_API_TOKEN` is set — the
MCP endpoints (`/mcp/sse`, `/mcp/message`). The hourly trash auto-purge
daemon also runs out of this command.

```bash
photo-sorter serve [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--port` | int | `8080` | Server port (TCP listener) |
| `--host` | string | `0.0.0.0` | Server bind address |
| `--session-secret` | string | `""` | Secret for signing session cookies. When unset, falls back to `WEB_SESSION_SECRET`; if that is also unset a random per-process secret is used and the server logs a startup WARN. |

**Environment-variable overrides** (read after the flags, so they win
when both are set):

| Variable | Description |
|----------|-------------|
| `WEB_PORT` | Override `--port` |
| `WEB_HOST` | Override `--host` |
| `WEB_SESSION_SECRET` | Override `--session-secret` |
| `MCP_API_TOKEN` | If set, mounts the MCP server at `/mcp/sse` + `/mcp/message`. If unset the routes are not registered. |
| `TRASH_RETENTION_DAYS` | Retention window for the hourly auto-purge daemon (default `30`). Invalid values fall back to the default with a WARN. |
| `PHOTOPRISM_URL` | Required — `serve` boots the legacy PhotoPrism client used by a handful of bridge handlers and refuses to start without it. |

**Example:**
```bash
photo-sorter serve --port 3000
```

**External decoders:** `serve` runs a startup check for `dcraw`,
`heif-convert`, and `exiftool` and logs a WARN line for each missing
binary. Missing binaries make RAW/HEIC uploads or XMP sidecar writes
fail loud at request time rather than silently.

**Similarity search:** the server uses pgvector's native HNSW indexes
on `embeddings.embedding` and `faces.embedding` (operator class
`vector_cosine_ops`). pgvector maintains them automatically on INSERT /
UPDATE / DELETE — there is no in-process index, no on-disk file, and
no startup or shutdown overhead beyond opening / closing the DB pool.
See [`docs/similarity-search.md`](similarity-search.md).

**MCP server (integrated):** when `MCP_API_TOKEN` is set, the MCP
endpoints are mounted on the same HTTP server:

```bash
export MCP_API_TOKEN=my-secret-token
photo-sorter serve --port 8085
# MCP available at http://localhost:8085/mcp/sse
# Web UI available at http://localhost:8085/
```

MCP clients authenticate with `Authorization: Bearer <MCP_API_TOKEN>`.
For the tool catalogue and parameter shapes see
[API §MCP Server](API.md#mcp-server).

---

### users

Manage the local user accounts that back the web UI login. Useful for
bootstrapping a fresh install, resetting a forgotten admin password, or
removing a stale account when the web UI is unreachable.

Every mutating subcommand (`create`, `set-password`, `delete`) appends
a row to the `audit_log` table with `metadata.actor = "cli"` so CLI
activity shows up in the admin audit viewer alongside web traffic.

#### users list

List all local users.

```bash
photo-sorter users list [--json]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Output as JSON instead of a table |

#### users create

Create a new user. The password is read from the terminal (hidden,
confirmed twice). Refuses to run when stdin is not a TTY — scripted
callers should hit the REST API instead.

```bash
photo-sorter users create <username> --role=<admin|editor|viewer> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--role` | string | *(required)* | One of `admin`, `editor`, `viewer` |
| `--display-name` | string | username | Human-friendly display name |
| `--email` | string | `""` | Email address (optional) |

Username must match `[a-z0-9_.-]{3,64}`; password must be at least
8 characters.

**Example:**
```bash
photo-sorter users create alice --role=editor --display-name="Alice"
```

#### users set-password

Reset a user's password. Prompts for the new password twice on the
terminal. Refuses to run when stdin is not a TTY.

```bash
photo-sorter users set-password <username>
```

#### users delete

Delete a user account. Prompts for confirmation unless `--yes` is given.
Refuses to delete the only remaining enabled admin (same invariant the
REST handler enforces).

```bash
photo-sorter users delete <username> [--yes]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--yes` | bool | `false` | Skip the confirmation prompt |

---

### api-tokens

Manage the long-lived, **read-only** bearer tokens used by machine clients —
principally the Kukátko migration exporter, which walks the whole library over
the HTTP API.

A session cookie is the wrong credential for a bulk export: it expires after 30
days and is backed by a `sessions` row that the cleanup loop eventually
deletes, so a long-running job can die halfway through. An API token has no
such lifetime.

The token is read-only, enforced three independent ways: it authenticates as
the `viewer` role (which fails both `auth.HasWriteAccess` and
`handlers.requireWriteRole`), and `RequireAuth` additionally rejects any
non-`GET`/`HEAD`/`OPTIONS` request from a token principal outright — that last
gate holds even for a handler that checks nothing itself.

There is deliberately **no REST surface** for creating tokens: a credential
able to mint further credentials is exactly what a read-only export must not
have. Minting and revoking append a row to `audit_log` with
`metadata.actor = "cli"`.

#### api-tokens create

Mint a token and print it once.

```bash
photo-sorter api-tokens create <name> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--expires-in` | duration | `0` | Token lifetime (e.g. `720h`). Zero means it never expires. |
| `--json` | bool | `false` | Output as JSON instead of human-readable text |

```bash
# Never expires — what a migration run wants.
$ photo-sorter api-tokens create kukatko-migration
API token created.

  UID:     t3k9x2mq7n4vb8zc
  Name:    kukatko-migration
  Scope:   read (read-only)
  Expires: never

  Token:   psat_Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MGFiY2RlZmdoaWo

This is the only time the token is shown — copy it now.
Use it as:  curl -H 'Authorization: Bearer psat_...' ...
```

The raw token is printed **once** and never stored — only its SHA-256 goes into
`api_tokens.token_hash`. SHA-256 rather than bcrypt is deliberate: the token is
256 bits of `crypto/rand`, so there is no low-entropy structure to
brute-force, and it is verified on *every* request — a bcrypt round per request
would add ~100 ms of CPU to a 20k-photo export. A lost token cannot be
recovered; revoke it and mint a new one.

#### api-tokens list

List every token, newest first. Revoked and expired tokens are included so the
history stays visible.

```bash
photo-sorter api-tokens list [--json]
```

```
UID               NAME               SCOPE  STATE    EXPIRES  LAST USED             CREATED
---               ----               -----  -----    -------  ---------             -------
t3k9x2mq7n4vb8zc  kukatko-migration  read   active   never    2026-07-14T09:12:33Z  2026-07-14T08:00:00Z
t7f2p1ab5cd9ef3g  ci-export          read   revoked  never    -                     2026-06-01T10:00:00Z
```

#### api-tokens revoke

Soft-revoke a token by UID. The row is kept so the audit trail still shows the
token existed. Revocation takes effect immediately — liveness is re-checked in
SQL on every request.

```bash
photo-sorter api-tokens revoke <uid>
```

---

### photo

Photo operations and information. Subcommands work on individual
photos or photo collections.

#### photo info

Display detailed information about a photo including metadata and
perceptual hashes (pHash and dHash) for similarity matching. Photo
data is downloaded from PhotoPrism, hashes are computed locally.

```bash
photo-sorter photo info <photo-uid> [flags]
photo-sorter photo info --album <album-uid> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--album` | string | `""` | Process all photos in an album (mutually exclusive with the positional `photo-uid`) |
| `--json` | bool | `false` | Output as JSON |
| `--limit` | int | `0` | Limit number of photos in album mode (`0` = no limit) |
| `--concurrency` | int | `5` | Number of parallel workers |

**Examples:**
```bash
# Single photo
photo-sorter photo info pq8abc123def

# Album with JSON output
photo-sorter photo info --album aq8xyz789 --json
```

#### photo match

Find every photo containing a specific person by comparing face
embeddings stored in PostgreSQL against the candidate person's seed
faces fetched from PhotoPrism (`q=person:<name>`). With `--apply`, the
command writes back to PhotoPrism: creating missing markers and
assigning the person to existing-but-unassigned markers. *Legacy* —
the native UI exposes the same operations via `POST /api/v1/faces/match`
and `POST /api/v1/faces/apply`.

```bash
photo-sorter photo match <person-name> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--threshold` | float64 | `0.5` | Maximum cosine distance for face matching (lower = stricter) |
| `--limit` | int | `0` | Limit number of results (`0` = no limit) |
| `--json` | bool | `false` | Output as JSON |
| `--apply` | bool | `false` | Apply changes to PhotoPrism (create markers and assign the person) |
| `--dry-run` | bool | `false` | Preview the apply pass without writing (use with `--apply`) |
| `--save-matches` | bool | `false` | Write matched photos with face boxes drawn to `test/matches/<uid>.jpg` (debug aid) |

**Examples:**
```bash
# Find all photos containing john-doe
photo-sorter photo match john-doe

# Stricter matching, capped results
photo-sorter photo match john-doe --threshold 0.4 --limit 100

# Preview the apply pass
photo-sorter photo match john-doe --apply --dry-run

# Actually create markers / assign people
photo-sorter photo match john-doe --apply

# Machine-readable
photo-sorter photo match john-doe --json
```

#### photo similar

Find photos similar to a given photo (by UID) or to every photo
carrying a given label (by `--label`). Uses cosine distance over the
CLIP image embeddings in `embeddings` (pgvector HNSW). With `--apply`
the matched photos receive the labels in PhotoPrism. *Legacy* — the
native UI exposes the same operations via
`POST /api/v1/photos/similar` and `POST /api/v1/photos/similar/collection`.

```bash
photo-sorter photo similar <photo-uid> [flags]
photo-sorter photo similar --label <name> [--label <name>...] [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--threshold` | float64 | `0.3` | Maximum cosine distance for similarity (lower = more similar) |
| `--limit` | int | `50` | Maximum number of results |
| `--json` | bool | `false` | Output as JSON |
| `--label` | string[] | `[]` | Find photos similar to every photo carrying this label (repeatable). Mutually exclusive with the positional `photo-uid`. |
| `--apply` | bool | `false` | Apply the label(s) to similar photos found (`--label` mode only) |
| `--dry-run` | bool | `false` | Preview label assignments without applying them (use with `--apply`) |

**Examples:**
```bash
# Single photo
photo-sorter photo similar pq8abc123def

# Find every photo similar to photos already tagged "cat"
photo-sorter photo similar --label cat

# Multiple seed labels, stricter threshold
photo-sorter photo similar --label cat --label dog --threshold 0.2

# Preview label propagation
photo-sorter photo similar --label cat --apply --dry-run

# Apply labels for real
photo-sorter photo similar --label cat --apply
```

#### photo clear-faces

Delete face markers from a PhotoPrism photo. By default removes every
face marker (both assigned and unassigned); pass `--assigned-only` to
keep raw detections and only strip person assignments. *Legacy.*

```bash
photo-sorter photo clear-faces <photo-uid> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | bool | `false` | List markers that would be deleted without writing |
| `--assigned-only` | bool | `false` | Only delete markers that have a person assigned |

**Examples:**
```bash
# Delete every face marker on the photo
photo-sorter photo clear-faces pt4abc123def

# Only delete markers with a person assigned
photo-sorter photo clear-faces pt4abc123def --assigned-only

# Preview without writing
photo-sorter photo clear-faces pt4abc123def --dry-run
```

---

### cache

Cache management commands. Most subcommands target the native
PostgreSQL store written by the upload pipeline; the two PhotoPrism
subcommands are explicitly flagged below.

#### cache build-thumbs

Generate any missing thumbnails for every photo in the `photos` table.
Used after a migration, after a cache wipe, or whenever a new size
definition is added to the registry. Reuses
`internal/thumb.GenerateSizes` (decode once, write every requested
size). For each photo the original is resolved via storage; HEIC/RAW
originals are funnelled through `imgconvert.EnsureDecodable`
(`heif-convert` / `dcraw`) before resizing.

```bash
photo-sorter cache build-thumbs [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--concurrency` | int | `4` | Number of parallel decode + resize workers |
| `--sizes` | strings | *(all)* | Comma-separated subset of registered sizes (e.g. `fit_720,tile_224`). Unknown names error out. |
| `--limit` | int | `0` | Maximum number of photos to process (`0` = unlimited) |
| `--only-missing` | bool | `true` | Only generate thumbs not already cached. Pass `--only-missing=false` to force regeneration (existing thumbs are deleted first). |
| `--photo-uid` | string | `""` | Regenerate thumbs for a single photo by UID (short-circuits the listing pass and overrides `--limit`) |
| `--json` | bool | `false` | Output the run summary as JSON instead of a progress bar |

**Output counts** (`generated`, `skipped`, `failed`): `generated`
counts individual thumbnail files written, not photos. Per-photo
failures are logged to stderr and the run continues.

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

REST equivalent: `POST /api/v1/process/build-thumbs` (admin only).

**Prerequisites:**

- `DATABASE_URL` must be set.
- `STORAGE_ORIGINALS_PATH` / `STORAGE_CACHE_PATH` must point at the
  originals tree the upload pipeline writes to.
- `heif-convert` / `dcraw` must be installed for HEIC / RAW originals.

**Idempotency:** with `--only-missing` on (the default), re-runs are
safe — photos whose thumbs are all cached are skipped without rewriting.

#### cache compute-phashes

Backfill perceptual hashes (pHash + dHash) for photos that have no row
in `photo_phashes`. New uploads write the row automatically; this
command fills the gap for older photos. Idempotent — re-runs only
re-hash photos that have been added since the previous invocation.

```bash
photo-sorter cache compute-phashes [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--limit` | int | `0` | Maximum number of photos to process (`0` = unlimited) |
| `--concurrency` | int | `4` | Number of parallel decode + hash workers |
| `--json` | bool | `false` | Output the run summary as JSON |

**Examples:**
```bash
# Backfill every photo missing a pHash
photo-sorter cache compute-phashes

# First-run smoke test on the first 1000 photos
photo-sorter cache compute-phashes --limit 1000

# JSON output for scripting
photo-sorter cache compute-phashes --json
```

**What it does:**

1. Selects every `photos.uid` that has no row in `photo_phashes`.
2. For each photo (parallelised):
   - Resolves the on-disk primary file via the configured storage tree.
   - Runs `imgconvert.EnsureDecodable` so HEIC/RAW originals are
     funnelled through `heif-convert` / `dcraw` into a JPEG-friendly
     intermediate.
   - Computes pHash + dHash via `internal/fingerprint`.
   - Upserts the row into `photo_phashes`.
3. Reports counts of `(hashed, skipped, errored)`.

**Prerequisites:** `DATABASE_URL`, `STORAGE_ORIGINALS_PATH`,
`STORAGE_CACHE_PATH`.

**Force a full re-hash:** truncate `photo_phashes` and re-run:

```sql
TRUNCATE photo_phashes;
```

#### cache compute-eras

Compute CLIP text embedding centroids for the photo-era estimation
feature. For each of the 12 eras the command generates ~30 text prompts
describing typical visual cues of photos from that era, computes their
CLIP text embeddings via the embedding service (`POST /embed/text`),
averages them into an L2-normalised centroid, and stores the centroid
in the `era_embeddings` table. After the run, stale eras (centroids
whose slug is no longer in the current list) are deleted so the table
stays in sync with the code.

```bash
photo-sorter cache compute-eras [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | bool | `false` | Compute embeddings but do not save to the database |
| `--json` | bool | `false` | Output the run summary as JSON |

**Examples:**
```bash
# Preview without saving
photo-sorter cache compute-eras --dry-run

# Compute and save era centroids
photo-sorter cache compute-eras

# JSON output
photo-sorter cache compute-eras --json
```

**Prerequisites:** `DATABASE_URL`, `EMBEDDING_URL` (defaults to
`http://localhost:8000`). See
[`docs/era-estimation.md`](era-estimation.md) for the math.

#### cache push-embeddings *(legacy PhotoPrism)*

Push InsightFace (buffalo_l / ResNet100) face embeddings from the local
PostgreSQL cache into PhotoPrism's MariaDB, replacing the default
TensorFlow embeddings on `markers.embeddings_json`. Optionally
recomputes face-cluster centroids from the new embeddings. Requires
`PHOTOPRISM_DATABASE_URL` (MariaDB DSN) and `DATABASE_URL`. *Legacy* —
only meaningful for operators still running a PhotoPrism + MariaDB pair;
the native pipeline does not use it.

```bash
photo-sorter cache push-embeddings [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--dry-run` | bool | `false` | Preview changes without writing to MariaDB |
| `--recompute-centroids` | bool | `false` | Also recompute face-cluster centroids from the new embeddings |
| `--json` | bool | `false` | Output as JSON |

**Examples:**
```bash
photo-sorter cache push-embeddings --dry-run
photo-sorter cache push-embeddings
photo-sorter cache push-embeddings --recompute-centroids
```

#### cache sync *(legacy PhotoPrism)*

Refresh cached face marker metadata (marker UID, subject UID, subject
name, photo dimensions / orientation) from PhotoPrism for every face in
the local PostgreSQL cache. Use this after faces were assigned or
unassigned directly in the PhotoPrism UI so the local cache catches
up. *Legacy* — the native pipeline uses
`POST /api/v1/process/sync-cache` instead, which derives the same
metadata from the native `markers` table.

```bash
photo-sorter cache sync [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--concurrency` | int | `20` | Number of parallel workers (default `constants.WorkerPoolSize`) |
| `--json` | bool | `false` | Output the run summary as JSON instead of a progress bar |

**Examples:**
```bash
photo-sorter cache sync
photo-sorter cache sync --concurrency 5
photo-sorter cache sync --json
```

---

### backup

Create a timestamped backup containing the originals tree and a
`pg_dump` of the photo-sorter Postgres database. The thumbnail cache
is intentionally excluded because it can be regenerated from the
originals via `cache build-thumbs`. For the full DR runbook see
[`docs/backup.md`](backup.md).

```bash
photo-sorter backup --output <dir> [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--output` | string | *(required)* | Directory where backups are written |
| `--originals-path` | string | `$STORAGE_ORIGINALS_PATH` | Path to the originals tree |
| `--db-url` | string | `$DATABASE_URL` | Postgres connection URL passed to `pg_dump` |
| `--keep` | int | `14` | Number of backups to retain (`0` disables pruning) |
| `--compress` | string | `zstd` | Compression algorithm: `zstd` or `gzip` |
| `--skip-originals` | bool | `false` | Skip the originals tar |
| `--skip-db` | bool | `false` | Skip the `pg_dump` |
| `--cleanup-on-failure` | bool | `false` | Remove the `.tmp` directory if the run fails |
| `--progress-every` | int | `500` | Originals progress cadence (files between log lines) |

**Output layout:** each run produces `<output>/photo-sorter-<YYYYMMDD-HHMMSS>/`
containing three sibling files so they can be restored selectively:

- `metadata.json` — `{ created_at, sorter_version, db_size_bytes, originals_bytes, file_count }`.
- `db.sql.zst` (or `.gz`) — `pg_dump --format=plain --no-owner --no-privileges` piped through the compressor.
- `originals.tar.zst` (or `.tar.gz`) — streamed tar of the originals directory, preserving relative paths.

The directory is written first to `.photo-sorter-<ts>.tmp/` and only
renamed atomically once both artifacts succeed. Failed runs leave the
`.tmp` directory in place unless `--cleanup-on-failure` is set, so an
operator can inspect partial output. `--skip-originals` and `--skip-db`
cannot both be set in the same invocation.

**Requirements:**

- `pg_dump` must be on PATH (`apt: postgresql-client`; the Docker image
  bundles it).

**Examples:**

```bash
# Daily backup keeping the last 14 runs.
photo-sorter backup --output /var/backups/photo-sorter --keep 14

# Originals only (no database access).
photo-sorter backup --output /tmp/bak --skip-db

# Database only (no large file scan).
photo-sorter backup --output /tmp/bak --skip-originals

# Gzip instead of zstd, retain only 3 runs.
photo-sorter backup --output /tmp/bak --compress gzip --keep 3
```

#### Scheduling with systemd

Sample units live in [`deploy/systemd/`](../deploy/systemd/). Install
with:

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

The timer fires `photo-sorter-backup.service` daily at 03:00 (with a
10-minute random jitter and persistence across reboots).

---

### DB Commands

The `db-export` / `db-import` pair covers the metadata side of disaster
recovery: embeddings, faces, books, users, sessions, era_embeddings,
photos, albums, labels, markers, subjects — every row that lives in the
`photosorter` database. The on-disk originals tree is NOT part of these
commands; back it up separately via `rsync` / `borg` / etc., or via the
higher-level `backup` command above which bundles both into one
timestamped directory.

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
| `--output`, `-o` | string | `photosorter-<UTC timestamp>.<ext>` | Destination file path. The extension is chosen to match the format. |
| `--format` | string | `custom` | Dump format: `custom` (pg_dump's compressed binary) or `plain` (SQL) |
| `--no-compress` | bool | `false` | For `plain` format, skip gzipping the output. Ignored for `custom` (already compressed). |
| `--force` | bool | `false` | Overwrite the output file if it already exists |

The `custom` format is recommended: it is what `pg_restore` expects and
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
| `--input`, `-i` | string | *(required)* | Source dump file |
| `--yes`, `-y` | bool | `false` | Skip the interactive confirmation prompt |
| `--drop-existing` | bool | `false` | `DROP SCHEMA public CASCADE; CREATE SCHEMA public;` before restoring (clean slate) |

The dump format is auto-detected from the file header — `PGDMP` magic
bytes route to `pg_restore`; anything that looks like SQL goes to
`psql`. Gzipped files are decompressed transparently. For `custom`
format dumps that are also gzipped, the import gunzips to a temp file
first (`pg_restore` needs a seekable file) and removes it afterwards;
the gunzip is capped at 50 GiB as a gzip-bomb guard.

If the target database already contains data (the `embeddings` table
has rows), `db-import` refuses to overwrite it without `--yes`. The
interactive prompt requires the literal token `yes`.

For `custom` format dumps, `db-import` invokes `pg_restore --clean
--if-exists` unless `--drop-existing` is set; for `plain` format
dumps, the SQL emitted by `pg_dump` (or by `db-export --format plain`)
handles the drop/create ordering itself.

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

1. Restart the photo-sorter server (HNSW indexes load lazily as the
   pool warms up).
2. If the imported DB came from a host with a different originals tree,
   verify `STORAGE_ORIGINALS_PATH` and re-run `photo-sorter cache
   build-thumbs` to regenerate thumbnails for any missing sizes.

---

### migrate-from-photoprism

One-shot import of an existing PhotoPrism instance into photo-sorter's
native PostgreSQL schema and storage tree. Reads PhotoPrism's MariaDB
and copies primary originals from the source filesystem. Idempotent:
photos are skipped by SHA256 `file_hash`; subjects, labels, and albums
are looked up by name/slug before being created; markers are only
created for newly-inserted photos. A re-run prints "Created=0" for
every stage.

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
| `--pp-db` | string | *(required)* | PhotoPrism MariaDB DSN (`user:pw@tcp(host:3306)/db`) |
| `--pp-originals` | string | *(required)* | Path to PhotoPrism's originals directory |
| `--pp-cache` | string | `""` | PhotoPrism storage/cache dir (currently unused; reserved) |
| `--uploader-username` | string | `""` | Native photo-sorter username to write into `photos.uploaded_by`. Empty leaves `NULL`. |
| `--dry-run` | bool | `false` | Walk PhotoPrism and print counts; no DB writes, no file copies |
| `--skip-thumbs` | bool | `false` | Skip thumbnail regeneration |
| `--batch-size` | int | `200` | Source DB query batch size |
| `--concurrency` | int | `4` | Thumbnail-generation worker count |
| `--only` | strings | *(all)* | Limit stages: `subjects`, `photos`, `labels`, `albums`, `markers`, `thumbs` |
| `--emit-photo-map` | string | `""` | Optional path: after the photos stage, write a JSON dump of the PhotoPrism→native photo-UID map (consumed by `migrate-remap-references`). Identity map in the happy path. |

**UID preservation:** PhotoPrism UIDs for photos, albums, subjects, and
markers are written verbatim into the native `uid` columns. Cached
PhotoPrism references in `embeddings`, `faces` (`subject_uid`,
`marker_uid`), `section_photos`, and `page_slots` therefore reconnect
without a remap pass. Operators who already ran an older buggy version
of this command (which generated new UIDs) should follow the migration
with `migrate-remap-references --map <emit-file>`.

**Stages (in order):**

1. **subjects** — read PhotoPrism `subjects` and upsert into the native
   `subjects` table (idempotent on name).
2. **photos** — read `photos` joined with `files` / `cameras` / `lenses` /
   `details`; SHA256-hash each primary file; skip if `photos.file_hash`
   already exists; otherwise copy bytes into
   `STORAGE_ORIGINALS_PATH/YYYY/MM/<basename>`, insert the photo row,
   attach non-primary files.
3. **labels** — upsert labels by slug (priority < 0 excluded), then
   attach `photos_labels` rows with `source = "import"`.
4. **albums** — upsert albums by slug, then attach `photos_albums` rows
   (skipping already-linked photos).
5. **markers** — create face/label markers for newly-created photos.
6. **thumbnails** — regenerate every cached thumbnail size for photos
   created in this run (skipped via `--skip-thumbs`).

**Examples:**

```bash
# Dry-run against a live PhotoPrism (find missing files, estimate scope).
photo-sorter migrate-from-photoprism \
  --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
  --pp-originals /photoprism/originals \
  --uploader-username admin \
  --dry-run

# Full migration without regenerating thumbnails (run cache build-thumbs
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

The native uploader user must exist before the migration runs; create
one with `photo-sorter users create` (or leave `--uploader-username`
empty to write `NULL` `uploaded_by`). See
[`docs/migration-from-photoprism.md`](migration-from-photoprism.md) for
the full runbook.

---

### migrate-verify

Compare an existing PhotoPrism instance against the photo-sorter native
database after a migration. Read-only against both data sources; prints
(or emits as JSON) a two-phase diff:

1. **Structural** — counts, existence (`missing_in_sorter` /
   `orphan_in_sorter`), per-album/label photo memberships, marker
   geometry drift, on-disk orphan files.
2. **Field-level** — every column the migrator is supposed to copy is
   compared cell-by-cell. Field-level mismatches are reported as
   `field_diffs[]` per entity (JSON) and as colour-coded lines below
   the structural section (text). Tolerance bands swallow 1-second
   drift on dates, 1e-6 on lat/lng, 1 m on altitude, and 0.01 on
   marker score by default; `--strict` disables them.

Zero diffs from `migrate-verify` is the authoritative gate for
cancelling the PhotoPrism + MariaDB compose services. The exit code is
`0` when no diffs are reported and `1` otherwise, so the command
chains directly into CI / shell scripts.

```bash
photo-sorter migrate-verify \
  --pp-db "<DSN>" \
  --pp-originals <path> \
  [--json] [--no-color] [--concurrency <N>] [--fields-only] [--strict]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--pp-db` | string | *(required)* | PhotoPrism MariaDB DSN |
| `--pp-originals` | string | *(required)* | Path to the PhotoPrism originals directory (the verifier rehashes primary files) |
| `--json` | bool | `false` | Emit a machine-readable JSON report instead of the human-readable text |
| `--no-color` | bool | `false` | Disable ANSI colour escapes in the human-readable report (useful for log files / CI) |
| `--concurrency` | int | `verify.DefaultConcurrency` | Goroutine-pool size for the SHA256 re-hash pass |
| `--fields-only` | bool | `false` | Skip the structural existence / disk pass and run only the field-level diff. Useful when iterating on migrator fixes — the rehash pass dominates wall time on large libraries. |
| `--strict` | bool | `false` | Drop every tolerance band: 1-second drift on `taken_at`, 1e-6 on lat/lng, 1 m on altitude, 0.01 on marker score all become diffs |

**Sections in the report:**

1. **photos** — PhotoPrism row count vs sorter `photos` count. Each PP
   primary file is re-hashed with SHA256 and looked up by `file_hash`;
   misses go into `missing_in_sorter`. The reverse pass flags sorter
   rows whose hash has no PhotoPrism counterpart as `orphan_in_sorter`.
   Field diff covers `taken_at` / `taken_at_source` / `time_zone` /
   `taken_at_offset`, `description` / `notes`, `keywords` (sorted +
   NFC-normalised), GPS (`lat` / `lng` / `altitude`), camera /
   lens (`camera_make` / `camera_model` / `lens_model`), exposure
   (`iso` / `f_number` / `exposure` / `focal_length`), dimensions
   (`width` / `height` / `orientation`), flags (`favorite` / `private` /
   `panorama` / `scan` / `quality`), and EXIF text (`exif_artist` /
   `exif_copyright` / `exif_license` / `exif_software`).
2. **albums** — slug + title parity, per-album symmetric photo diff,
   plus per-pair `membership_diffs`. Field diff covers `description`,
   `location`, `category`, `notes`, `filter`, `album_order`, `favorite`,
   `private`, `type`.
3. **labels** — slug + name parity, per-label photo-pair counts, and
   per-pair `membership_diffs`. Field diff covers `description`,
   `categories`, `priority`, `favorite`.
4. **subjects / markers** — accent-insensitive subject name match,
   per-subject marker count diffs, per-marker geometry drift (markers
   whose x/y/w/h differs by more than 1% on any axis). Subject field
   diff covers `bio`, `about`, `alias`, `favorite`, `private`, `type`.
   Marker field diff covers `score` (≤ 0.01 tolerance), `invalid`,
   `reviewed`, and `subject_uid` linkage.
5. **disk** — orphan files: every regular file under the sorter's
   originals root that has no corresponding `photos.file_path` row.

JSON output: each entity's `field_diffs[]` is capped at 1000 entries
per field name (a single noisy field cannot crowd out diffs from
others). The human-readable output samples the first 50 entries per
entity and prints `...and N more truncated (use --json for the full
report)` when the bucket overflowed.

**Examples:**

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

# Machine-readable JSON, for jq / CI integration.
photo-sorter migrate-verify \
  --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
  --pp-originals /photoprism/originals \
  --json > verify.json
```

---

### migrate-remap-references

Rewrite every soft `photo_uid` reference in the native database using
a photo-UID map (as emitted by
`migrate-from-photoprism --emit-photo-map`).

Intended for operators who landed an older buggy version of the
migrator that wrote generated UIDs into `photos` instead of preserving
the PhotoPrism UIDs. After the fixed migrator runs, the operator's
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
| `--map` | string | *(required)* | Path to the photo-UID map JSON file (`{"version":1,"photo_uid_map":{...},"file_uid_map":{...}}`) |
| `--dry-run` | bool | `false` | Run the UPDATEs and roll back; report row counts and orphan stats without writing |
| `--yes` | bool | `false` | Skip the interactive confirmation prompt |

**Tables rewritten** (every `(old, new)` pair in `photo_uid_map`):

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
"nothing to remap" and exits `0`.

After the UPDATEs the command runs an integrity audit and prints, per
table, how many rows now point at a `photo_uid` that does not match
any `photos.uid`. Non-zero is informational (some PhotoPrism originals
may have been deleted before the migration); the command does not
fail.

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

Print the binary's version, commit SHA, and build date — all three are
injected via `-ldflags` at compile time and exposed in the web UI
header next to the GitHub icon.

```bash
photo-sorter version
```

Sample output:

```
photo-sorter v0.42.0
  Commit: 1234abcd
  Built:  2026-05-23T12:00:00Z
```
