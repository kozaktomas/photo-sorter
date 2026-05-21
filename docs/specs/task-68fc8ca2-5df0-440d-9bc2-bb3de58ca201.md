# Plug Photo-Level Data-Loss Gaps in migrate-from-photoprism

An audit of `internal/migrate/stage_photos.go` against the PhotoPrism MariaDB schema found that several photo-level metadata fields are silently dropped during migration. This task closes those gaps so that re-running `migrate-from-photoprism` (the command is idempotent) backfills them onto already-migrated photos and a fresh migration captures them on the first pass.

Source-side fields currently lost:
- `details.keywords` (free-form tag list, user-edited in PhotoPrism web UI)
- `photos.photo_panorama`, `photos.photo_scan`, `photos.photo_quality` (UI hint / sort / filter flags)
- `photos.time_zone`, `photos.taken_at_offset` (so local-time display for travel photos is correct)
- `details.artist`, `details.copyright`, `details.license`, `details.software` (EXIF-ish fields that may have been edited in PhotoPrism)

Re-discoverable from the original file via re-extraction, hence NOT in scope here:
- `file_main_color`, `file_colors`, `file_chroma`, `file_luminance` — color analytics
- `places.place_*` — geocoded location names (will be re-derived later if/when reverse geocoding is added)

## Requirements

### 1. Database migration
Add a new migration file under `internal/database/postgres/migrations/` with the next free three-digit prefix. The HNSW-removal task and other queued tasks may also claim a prefix; pick whichever number is free at apply time.

Schema changes:
- Table `photos`:
  - `panorama BOOLEAN NOT NULL DEFAULT FALSE`
  - `scan BOOLEAN NOT NULL DEFAULT FALSE` (was the photo created from a scanner?)
  - `quality SMALLINT NOT NULL DEFAULT 0` (PhotoPrism's quality tier, range 0..7)
  - `time_zone TEXT NOT NULL DEFAULT ''` (IANA name like `Europe/Prague`)
  - `taken_at_offset INTEGER NOT NULL DEFAULT 0` (offset in seconds from UTC for the `taken_at` instant)
  - `exif_artist TEXT NOT NULL DEFAULT ''`
  - `exif_copyright TEXT NOT NULL DEFAULT ''`
  - `exif_license TEXT NOT NULL DEFAULT ''`
  - `exif_software TEXT NOT NULL DEFAULT ''`
  - `keywords TEXT[] NOT NULL DEFAULT '{}'`
- New GIN index on `keywords` for tag search: `CREATE INDEX IF NOT EXISTS photos_keywords_gin ON photos USING gin (keywords);`
- All changes guarded with `ADD COLUMN IF NOT EXISTS` (Postgres 9.6+ supports the IF NOT EXISTS clause on `ALTER TABLE`).

### 2. Go model + repository updates
- `internal/database/types.go` (or wherever the Photo struct lives in the native package): add corresponding Go fields. Use:
  - `Panorama bool`
  - `Scan bool`
  - `Quality int16`
  - `TimeZone string`
  - `TakenAtOffset int` (seconds)
  - `ExifArtist`, `ExifCopyright`, `ExifLicense`, `ExifSoftware string`
  - `Keywords []string`
- `internal/database/postgres/photos.go` (and any companion reader/writer files): include these columns in INSERT, UPDATE, and SELECT statements. Use pgx's array support for `Keywords` (`pgtype.TextArray` or the new pgx v5 native slice mapping — match what the rest of the file uses).
- Native repository methods (`GetPhoto`, `ListPhotos`, `UpdatePhoto`, the EXIF edit handler's writer, etc.) must round-trip the new fields.

### 3. `migrate-from-photoprism` stage updates
In `internal/migrate/stage_photos.go`:

Source SQL:
- Extend the existing photos+details JOIN to also pull:
  - `photos.photo_panorama AS panorama`
  - `photos.photo_scan AS scan`
  - `photos.photo_quality AS quality`
  - `photos.time_zone AS time_zone`
  - `photos.taken_at_offset AS taken_at_offset`
  - `details.subject_keywords` if present (column name may differ — verify against PhotoPrism schema; if the source column is `keywords`, comma-separated, parse to []string)
  - `details.artist`, `details.copyright`, `details.license`, `details.software`
- PhotoPrism stores keywords two ways: `details.keywords` as a comma-separated string AND a normalized `keywords` table joined via `photos_keywords`. Inspect the source DB to determine which is populated in the operator's instance and pick the one with non-empty data. If both populated, prefer the normalized join (more authoritative). Code defensively: union both sources if uncertain, deduplicate.
- For `time_zone`: PhotoPrism may store empty string when unknown; map empty → empty. Do not invent a default like `UTC`.

Idempotency:
- The migrator currently skips photos whose `file_hash` already exists in the destination. THIS BEHAVIOUR MUST CHANGE for the new fields: on a re-run, if the photo already exists, the stage must compare each new field and `UPDATE` the row when the destination value is the default zero-value AND the source has a non-default. This way a re-run after this task lands backfills already-migrated photos without touching ones a user may have manually edited.
- Concretely: for each "skipped" photo path in the current code, instead branch into a "backfill" path that runs an UPDATE selecting only the columns this task introduced. Do NOT update other columns (preserve user edits).
- Document this in a comment block at the top of the stage so the next maintainer understands the merge semantics.

### 4. API surface
Audit the native photo REST endpoints — `GET /api/v1/photos/{uid}` and `GET /api/v1/photos` — and ensure the response payload exposes the new fields. The frontend already has consumers (e.g., the EXIF edit modal) that should now show keywords / scan / panorama flags read-only at minimum. Do not build new UI in this task; just make sure the JSON envelope carries the data so a future UI task can pick it up. Add the JSON field names to `web/src/types/index.ts` under the photo type.

### 5. EXIF edit endpoint
`PUT /api/v1/photos/{uid}/exif` should accept the four EXIF-ish fields (`exif_artist`, `exif_copyright`, `exif_license`, `exif_software`), `keywords`, `panorama`, and `scan`. Add these to the request payload struct, the validation block, the writer call, and the XMP sidecar write (exiftool subprocess) so the fields are persisted to BOTH the DB row AND the XMP sidecar next to the original. `quality` and `taken_at_offset` / `time_zone` should be readable but NOT settable via this endpoint — they are derived from EXIF / set by upload and not meant for manual edit. (Reject those keys with a clear error if present in the request.)

### 6. Tests
- Update `internal/migrate/migrate_test.go` (or add a new file `stage_photos_test.go`) to cover:
  - First-run migration writes all new fields.
  - Second-run migration leaves existing non-default values alone but fills default ones.
  - Keywords with embedded commas, diacritics, and emojis are preserved verbatim.
  - Time zone is preserved when source has it; empty when not.
- Update the EXIF endpoint test (look in `internal/web/handlers/`) to verify the new field round-trip.
- `make check` must remain green.

## Edge Cases
- PhotoPrism `details.keywords` may contain whitespace-padded entries (`"foo, bar , baz"`). Trim each token and drop empties before storing.
- `photo_quality` ranges 0..7 in PhotoPrism but the column is `INT` upstream; clamp to `[0, 7]` before insert (defensive, schema also enforces SMALLINT).
- `time_zone`: validate against Go's `time.LoadLocation` and fall back to empty string + log a warning if invalid. Do not refuse the migration over a bad TZ value.
- `taken_at_offset`: PhotoPrism stores this in minutes for older versions and seconds for newer; verify which by inspecting a few rows. Convert to seconds. If unsure, write the spec comment "Verified source unit on 2026-MM-DD: <seconds|minutes>" so a future audit can re-check.
- Stage `--only` flag continues to work: `--only photos` should backfill the new columns on existing photos without touching subjects/albums/markers.

## Verification
- `make build` succeeds.
- `make check` passes.
- Spin up the dev DB, run `migrate-from-photoprism --dry-run`, confirm the row counts for photos / details still match.
- Run a real migration against a snapshot of the operator's PhotoPrism. For 5 random sample photos, `SELECT keywords, panorama, scan, quality, time_zone, taken_at_offset, exif_artist, exif_copyright, exif_license, exif_software FROM photos WHERE file_hash = ?` returns the same values as `SELECT details.keywords, photos.photo_panorama, ... FROM photos JOIN details ON details.photo_id = photos.id WHERE photos.photo_uid = ?` in PhotoPrism.
- Re-run the migration: confirm no row is duplicated, but rows with the default values get filled in. Rows the user already edited keep their custom values.
- `PUT /api/v1/photos/{uid}/exif` with a keywords array updates both DB and the XMP sidecar (verify with `exiftool -Keywords <file>.xmp`).
