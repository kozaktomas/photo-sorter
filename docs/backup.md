# Backup and Restore

Operator runbook for backing up and restoring a photo-sorter
installation. Follow it top-to-bottom and you should be able to rebuild
a working install on a fresh host from cold storage with nothing more
than the binary and these instructions.

## Overview

Photo-sorter splits its persistent state into two categories: **archival**
state (originals and the Postgres database) that must be backed up
because it cannot be regenerated, and **regenerable** state (thumbnail
cache, LaTeX temp output) that is derived from the archival state and
should be deliberately excluded from backups so they stay small and
fast.

## What to back up

| Path / source | What it contains | Backup? | Notes |
|---------------|------------------|---------|-------|
| `$STORAGE_ORIGINALS_PATH` (e.g. `/data/originals/YYYY/MM/*`) | The unmodified primary files plus the `.xmp` sidecars written by EXIF edits. | **Yes** | Layout is `YYYY/MM/<filename>`. The sidecars sit next to the originals (`<basename>.xmp`) and are picked up automatically by any file-tree copy. |
| PostgreSQL `photosorter` database | Photos, albums, labels, subjects, markers, faces, embeddings, era centroids, photo books, text-check results, users, sessions — and the pgvector HNSW indexes (recreated as part of the schema during restore). | **Yes** | Single source of truth for all metadata. Dump with `photo-sorter db-export` (wraps `pg_dump`). |
| `$STORAGE_CACHE_PATH/thumb/...` | Hash-sharded thumbnail cache (`thumb/<aa>/<bb>/<cc>/<hash>_<size>.jpg`). | No | Fully regenerable from the originals via `photo-sorter cache build-thumbs`. Skipping this typically saves more space than the originals themselves. |
| LaTeX / PDF temp output | `internal/latex` build directories under the OS temp dir, plus any compiled book PDFs the operator chose to keep. | No | Recreated on the next book export. |

There is no separate on-disk HNSW index file. Vector search runs against
pgvector HNSW indexes inside Postgres (see
[`similarity-search.md`](similarity-search.md)), so dumping the database
captures the indexes implicitly.

## Backup procedure

The recommended setup is one job that snapshots the originals to a remote
location and a second job that dumps the database to the same location.
Both are cheap; both can run while the server is online.

### Step 1 — Snapshot the originals tree

`rsync` to a remote host (mirror copy, delete vanished files):

```bash
rsync -aH --delete \
  /data/originals/ \
  backup@backup-host:/srv/photo-sorter/originals/
```

`borg` for a deduplicated, encrypted archive:

```bash
borg create \
  --stats --compression zstd \
  /srv/borg/photo-sorter::originals-{utcnow:%Y%m%dT%H%M%S} \
  /data/originals
```

XMP sidecars (`<basename>.xmp`) live next to the originals and are picked
up automatically by either command — no extra flags needed.

### Step 2 — Dump the metadata database

`photo-sorter db-export` is shipped with the binary and wraps `pg_dump`
in pg_dump's native custom format (binary, compressed, the preferred
input for `pg_restore`). It needs `DATABASE_URL` in the environment and
`pg_dump` on `PATH`.

```bash
photo-sorter db-export \
  -o /backups/photosorter-$(date -u +%Y%m%d-%H%M%S).dump
```

For a human-readable, gzipped SQL file instead (handy for `grep`):

```bash
photo-sorter db-export \
  --format plain \
  -o /backups/photosorter-$(date -u +%Y%m%d-%H%M%S).sql.gz
```

`pg_dump` runs in snapshot isolation, so the dump is internally
consistent even while the server is accepting writes — there is no need
to stop `photo-sorter serve` first.

### Step 3 — Bundled CLI (optional)

If you prefer a single command that produces a timestamped directory
holding both halves, with retention pruning, use the higher-level
wrapper:

```bash
photo-sorter backup \
  --output /var/backups/photo-sorter \
  --keep 14
```

Each run produces
`<output>/photo-sorter-<YYYYMMDD-HHMMSS>/{metadata.json,db.sql.zst,originals.tar.zst}`.
See the [`README.md`](../README.md#backups) section for the systemd
timer that schedules it nightly.

### Step 4 — Automate via cron (rsync + db-export variant)

A single crontab entry that snapshots originals and dumps the DB nightly
at 03:00 (UTC):

```cron
0 3 * * * rsync -aH --delete /data/originals/ backup@backup-host:/srv/photo-sorter/originals/ && photo-sorter db-export -o /backups/photosorter-$(date -u +\%Y\%m\%d-\%H\%M\%S).dump
```

The `\%` escapes are required: `cron` interprets a bare `%` as end-of-line
for the command field.

## Restore procedure

You will need: the binary (matching or newer than the one that wrote the
dump — see [Schema version mismatch](#schema-version-mismatch) below),
the originals snapshot, and the database dump file.

### Step 1 — Provision PostgreSQL

Start a PostgreSQL 15+ instance with the `pgvector` extension and create
an empty `photosorter` database. The [`README.md`](../README.md#postgresql-setup)
walks through the recommended `pgvector/pgvector:pg17` Docker image.
Drop the `DATABASE_URL` into the environment of the host that will run
the restore commands:

```bash
export DATABASE_URL='postgres://photosorter:secret@localhost:5432/photosorter?sslmode=disable'
```

### Step 2 — Restore the originals tree

`rsync` from the backup target back into the configured originals root:

```bash
rsync -aH \
  backup@backup-host:/srv/photo-sorter/originals/ \
  /data/originals/
```

The DB stores **relative** paths under `STORAGE_ORIGINALS_PATH`, so the
originals just need to live at the new root. You can change
`STORAGE_ORIGINALS_PATH` between hosts without rewriting the DB.

### Step 3 — Restore the database

```bash
photo-sorter db-import \
  -i /backups/photosorter-20260520-030000.dump \
  --yes
```

If the target database has leftover rows (failed previous restore,
stale install), add `--drop-existing` to wipe the public schema before
loading the dump:

```bash
photo-sorter db-import \
  -i /backups/photosorter-20260520-030000.dump \
  --drop-existing \
  --yes
```

`db-import` detects the dump format (custom vs. plain, gzipped or not)
from the file header. The pgvector HNSW indexes are recreated as part
of the dump's `CREATE INDEX` statements — there is no separate rebuild
step.

### Step 4 — Start the server

```bash
photo-sorter serve
```

Migrations are applied at startup; on a freshly imported dump this is a
no-op. Watch the log for the `startup: external decoders OK` line that
confirms `exiftool`, `heif-convert`, and `dcraw` are on `PATH`.

### Step 5 — Regenerate thumbnails

The thumbnail cache was deliberately excluded from the backup. Either
backfill it eagerly:

```bash
photo-sorter cache build-thumbs --concurrency 4
```

…or trigger the same logic from the running server as an admin:

```bash
curl -b cookies.txt -X POST http://localhost:8085/api/v1/process/build-thumbs \
  -H 'Content-Type: application/json' \
  -d '{"concurrency": 4}'
```

While the backfill is in flight, the API returns `404` for individual
missing thumbnails. On a quiet install you can skip the backfill
entirely and let traffic pull thumbnails through `cache build-thumbs`
later; the originals are always served.

## Disaster recovery checklist

For 3am:

1. Provision PostgreSQL 15+ with pgvector; create the `photosorter`
   database; export `DATABASE_URL`.
2. `rsync` the originals snapshot to `STORAGE_ORIGINALS_PATH`.
3. `photo-sorter db-import -i <dump> --yes` (add `--drop-existing` if
   the target DB has leftover rows).
4. `photo-sorter serve` (verify the startup log shows decoders OK).
5. `photo-sorter cache build-thumbs --concurrency 4` (optional —
   safe to defer).
6. Log in with the original admin credentials and spot-check: open a
   recent album, open a face/person, open a photo book.

## What NOT to back up

| Path | Why skip | Cost to regenerate |
|------|----------|--------------------|
| `$STORAGE_CACHE_PATH/thumb/...` | Derived from the originals; rewritten on demand. | ~20 min for a 50k-photo library on a Raspberry Pi via `photo-sorter cache build-thumbs --concurrency 4`. Per-photo on cache miss is sub-second on first request. |
| `internal/latex` build dirs (`$TMPDIR/photo-sorter-latex-*`) | Per-export scratch space; deleted automatically when an export completes or is cancelled. | Recreated on the next PDF export. |
| `node_modules/`, `web/dist/`, Go build cache | Build artefacts; reproduced from source by `make build`. | Minutes on x86_64, ~10 min on Raspberry Pi. |

The pgvector HNSW indexes have sometimes been backed up out of habit as
separate `.hnsw` files. They are now part of the Postgres schema and do
not exist as independent files — there is nothing extra to capture.

## Edge cases

### Different `STORAGE_ORIGINALS_PATH` on the restore host

Paths to originals are stored relative to `STORAGE_ORIGINALS_PATH`, so
restoring to `/srv/photos` on a new host instead of `/data/originals`
on the old one just means setting `STORAGE_ORIGINALS_PATH=/srv/photos`
in the new host's environment. No database rewrite is required.

### Restoring a dump taken while the server was running

`pg_dump` runs inside a single transaction at `REPEATABLE READ`, so the
dump is a point-in-time snapshot even if the server is accepting writes
during the export. There is no need to stop `photo-sorter serve`
before `db-export`.

### Schema version mismatch

Photo-sorter migrations are forward-only and are applied automatically
at startup. If the dump was produced by a **newer** binary than the one
you are restoring with, `photo-sorter serve` will refuse to start
because it sees migration versions it does not know about. The fix is to
upgrade the binary on the restore host, not to downgrade the dump. The
reverse direction (older dump, newer binary) is supported: the binary
applies any pending migrations and starts normally.
