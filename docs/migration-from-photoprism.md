# Migration from PhotoPrism

This runbook walks an operator through replacing PhotoPrism with
photo-sorter while preserving photo UIDs, album / subject / marker
linkage, on-disk originals, and (optionally) cached thumbnails.

A zero-diff `migrate-verify` run is the gate for cancelling the
PhotoPrism + MariaDB compose services.

## UIDs are preserved verbatim

`migrate-from-photoprism` copies PhotoPrism UIDs across `photos`,
`albums`, `subjects`, and `markers` unchanged. The downstream native
tables that hold a soft `photo_uid` reference (`embeddings`, `faces`,
`section_photos`, `page_slots`) therefore stay valid out of the box —
no remap pass is needed in the happy path. `migrate-remap-references`
exists only as a recovery tool for operators who landed an earlier
buggy migrator that generated new UIDs (see Step 4's
`--emit-photo-map`).

## Migrated fields

The migrator is field-explicit, not "copy everything that looks
related". Each stage carries the columns below across from PhotoPrism;
anything not listed is either intentionally dropped or recomputed from
the originals on the native side. `migrate-verify`'s field-level diff
compares the same set.

- **photos** — title, description, taken_at + timezone, lat/lng/altitude,
  EXIF tags (camera, lens, exposure, ISO, focal length, GPS), `keywords`
  (union of `details.keywords` and the `photos_keywords` join table,
  deduplicated), the `scan` / `panorama` / `private` / `favorite` flags,
  and the `quality` score.
- **subjects** — name, bio, about, alias, type, favorite, private.
- **labels** — name, slug, description, categories, priority, favorite
  (PhotoPrism rows with priority < 0 are skipped on purpose).
- **albums** — title, description, location, category, notes, filter,
  order, favorite, private, type.
- **markers** — bounding box, score, invalid / reviewed flags,
  subject UID linkage.

## Prerequisites

- **PhotoPrism MariaDB reachable** from the host running `photo-sorter`.
  The migrator only reads from it; you can leave PhotoPrism running for
  the dry-run, but PhotoPrism must be **stopped** before the real
  migration so no new writes land mid-copy.
- **PhotoPrism's originals directory on the same host** as
  `STORAGE_ORIGINALS_PATH`. The migrator copies the primary file for
  each photo by re-hashing it (SHA256) and writing it under
  `STORAGE_ORIGINALS_PATH/YYYY/MM/<basename>`. Bind-mount paths are
  fine; remote / object-storage paths are not.
- **photo-sorter already deployed** with `DATABASE_URL`,
  `STORAGE_ORIGINALS_PATH`, and `STORAGE_CACHE_PATH` set, the bootstrap
  admin created, and the binary on `PATH`.
- **`pg_dump` on `PATH`** for the safety backup (`apt: postgresql-client`;
  the Docker image already ships it).

## Step 1 — Back up everything first

A backup is the only safety net. Take one before touching anything.

```bash
# Backup photo-sorter's own state (Postgres + originals tree).
photo-sorter backup --output /var/backups/photo-sorter --keep 14

# Capture PhotoPrism's MariaDB too — the migrator only reads from it,
# but a snapshot makes any rollback trivial.
mysqldump -h mariadb -u photoprism -pphotoprism photoprism \
  | zstd -o /var/backups/photoprism-pre-migrate.sql.zst
```

If the photo-sorter `photos` table is empty (first migration), the
photo-sorter backup will just record the schema — that's fine.

## Step 2 — Stop PhotoPrism (no writes during the migration)

The migrator is idempotent (re-runs skip rows that already exist), but
new PhotoPrism writes during the copy phase can land between the read
pass and the write pass and turn into orphans. Stop the PhotoPrism
container before the real run:

```bash
docker compose stop photoprism
```

You can leave MariaDB running — the migrator needs it.

## Step 3 — Dry-run to estimate scope

```bash
photo-sorter migrate-from-photoprism \
  --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
  --pp-originals /photoprism/originals \
  --uploader-username admin \
  --dry-run
```

The dry-run walks PhotoPrism and prints per-stage counts (photos,
albums, labels, subjects, markers, thumbnails). Use it to catch:

- Missing originals on disk (the migrator logs the basename of every
  primary file it can't open).
- Unexpected photo count (e.g. soft-deleted photos you forgot to purge).
- Database connectivity / credentials issues — fail fast here, not
  halfway through the real run.

No DB writes and no file copies happen in dry-run mode.

## Step 4 — Run the migration

```bash
photo-sorter migrate-from-photoprism \
  --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
  --pp-originals /photoprism/originals \
  --uploader-username admin
```

Stages (in order):

1. **subjects** — read PhotoPrism `subjects`, upsert into native `subjects` (idempotent by name).
2. **photos** — for each PhotoPrism `photos` row, SHA256-hash the primary file and skip if `photos.file_hash` already exists; otherwise copy the bytes into `STORAGE_ORIGINALS_PATH/YYYY/MM/<basename>`, insert the `photos` row, and attach non-primary files.
3. **labels** — upsert labels by slug (PhotoPrism rows with priority < 0 are skipped), then attach `photo_labels` rows with `source = "import"`.
4. **albums** — upsert albums by slug, then attach `album_photos` rows (skipping already-linked photos).
5. **markers** — create face / label markers for newly-created photos only (markers are presumed already attached for photos that existed before this run).
6. **thumbnails** — regenerate every registered thumbnail size for the photos created in this run (skipped via `--skip-thumbs`).

Re-runs are safe: photos are skipped by SHA256, subjects / labels /
albums are looked up by name / slug before insert, album_photos and
photo_labels are pre-checked, and markers are only created for newly-
inserted photos.

### Useful flags

- `--skip-thumbs` — skip stage 6 and run `cache build-thumbs` afterwards instead. Slightly faster overall when migrating a large library because `cache build-thumbs` can be re-run if it gets interrupted.
- `--only photos,labels` — limit stages (handy when retrying after a partial failure).
- `--batch-size 200`, `--concurrency 4` — tune throughput.
- `--emit-photo-map /tmp/photo-map.json` — write the PhotoPrism → native photo-UID map. In the happy path it's an identity map and nothing has to consume it; operators who landed an older buggy version of the migrator (which generated new UIDs) need this file to feed `migrate-remap-references`.

## Step 5 — Verify

```bash
photo-sorter migrate-verify \
  --pp-db "photoprism:photoprism@tcp(mariadb:3306)/photoprism" \
  --pp-originals /photoprism/originals
```

The verifier runs a two-phase diff:

1. **Structural** — row counts, existence (`missing_in_sorter` / `orphan_in_sorter`), per-album / label / subject memberships, marker geometry drift, on-disk orphan files.
2. **Field-level** — every column the migrator was supposed to copy is compared cell-by-cell with tolerance bands (1-second drift on `taken_at`, 1e-6 on lat/lng, 1 m on altitude, 0.01 on marker score). `--strict` drops the bands.

Run it without `--strict` first; a clean run there is the green light to
cut PhotoPrism off. If you need belt-and-braces certainty (e.g. before
deleting backups), re-run with `--strict`.

Exit code is 0 when nothing differs, 1 otherwise — chain it into your
ops scripts.

If `migrate-verify` reports diffs:

- **`missing_in_sorter`** — primary file on disk didn't hash to anything in `photos.file_hash`. Usually means the file failed to copy; re-run `migrate-from-photoprism` to retry.
- **`orphan_in_sorter`** — native row with no PhotoPrism counterpart. Usually a leftover from an earlier failed run or unrelated uploads; safe to ignore if it's expected.
- **`field_diffs`** — the source column was missed by the migrator. File a bug; do NOT cut PhotoPrism off until it's resolved.

## Step 6 — Backfill thumbnails (if you used `--skip-thumbs`)

```bash
photo-sorter cache build-thumbs
```

Decodes each original once and writes every registered thumbnail size
under `STORAGE_CACHE_PATH/thumb/...`. Idempotent; safe to interrupt and
re-run (only missing thumbs are touched).

## Step 7 — Cut PhotoPrism off

Once `migrate-verify` is clean and `cache build-thumbs` has finished:

1. **Drop the PhotoPrism + MariaDB services from `docker-compose.yml`.** They have no remaining runtime consumer — photo-sorter reads everything from its own Postgres + originals tree.
2. **Remove the `photoprism-test-*` and `mariadb-test-data` bind-mount volumes** once your post-migration backups have been verified restorable.
3. **Stop setting the `PHOTOPRISM_*` env vars** in `.env` / `.env.dev`. The runtime no longer reads them; the migration commands take their connection details from the `--pp-*` flags.

The PhotoPrism REST client at `internal/photoprism/` is retained only to
drive the migration commands themselves and can be removed at the
operator's convenience.

## Recovery

If something goes wrong mid-migration:

- The `photos` table is only touched on successful SHA256 matches — incomplete copies leave no orphan rows.
- Re-running `migrate-from-photoprism` resumes from the failure point (every stage is idempotent).
- Worst case, restore from the Step 1 backup with `photo-sorter backup` artifacts (the README has the restore commands) plus the MariaDB dump.

## See also

- [`docs/cli-reference.md`](cli-reference.md) — full flag reference for `migrate-from-photoprism`, `migrate-verify`, `migrate-remap-references`, `backup`, and `cache build-thumbs`.
- [`docs/architecture.md`](architecture.md) — what the post-migration deployment looks like.
- [`docs/markers.md`](markers.md) — how PhotoPrism's `markers` table maps to the native one.
