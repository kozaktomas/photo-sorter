-- Photo-level metadata fields previously dropped by migrate-from-photoprism.
--
-- See docs/specs/task-68fc8ca2-5df0-440d-9bc2-bb3de58ca201.md for the audit:
--   * keywords     -- free-form tag list (details.keywords + photos_keywords join)
--   * panorama, scan, quality      -- UI hint / sort / filter flags
--   * time_zone, taken_at_offset   -- local-time display for travel photos
--   * exif_artist / copyright /    -- EXIF-ish fields editable in PhotoPrism
--     license / software
--
-- Every column is added with `ADD COLUMN IF NOT EXISTS` so the migration is
-- idempotent under repeated runs and against partially-applied snapshots.
-- All new columns are NOT NULL with sensible zero-value defaults so existing
-- rows pick up the schema change without a backfill step; migrate-from-
-- photoprism then fills in the values for rows that were imported before
-- this migration landed.

ALTER TABLE photos ADD COLUMN IF NOT EXISTS panorama        BOOLEAN  NOT NULL DEFAULT FALSE;
ALTER TABLE photos ADD COLUMN IF NOT EXISTS scan            BOOLEAN  NOT NULL DEFAULT FALSE;
ALTER TABLE photos ADD COLUMN IF NOT EXISTS quality         SMALLINT NOT NULL DEFAULT 0;
ALTER TABLE photos ADD COLUMN IF NOT EXISTS time_zone       TEXT     NOT NULL DEFAULT '';
ALTER TABLE photos ADD COLUMN IF NOT EXISTS taken_at_offset INTEGER  NOT NULL DEFAULT 0;
ALTER TABLE photos ADD COLUMN IF NOT EXISTS exif_artist     TEXT     NOT NULL DEFAULT '';
ALTER TABLE photos ADD COLUMN IF NOT EXISTS exif_copyright  TEXT     NOT NULL DEFAULT '';
ALTER TABLE photos ADD COLUMN IF NOT EXISTS exif_license    TEXT     NOT NULL DEFAULT '';
ALTER TABLE photos ADD COLUMN IF NOT EXISTS exif_software   TEXT     NOT NULL DEFAULT '';
ALTER TABLE photos ADD COLUMN IF NOT EXISTS keywords        TEXT[]   NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS photos_keywords_gin ON photos USING gin (keywords);
