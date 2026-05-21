-- Subject / label / album metadata fields previously dropped by migrate-from-photoprism.
--
-- See docs/specs/task-332a727c-a66b-4922-8081-edbffffe5acd.md for the audit:
--   * subjects: bio / about / alias    -- PhotoPrism subj_bio / subj_about / subj_alias
--   * labels:   description / categories  -- label_description + label_categories (csv)
--   * albums:   location / category / notes / filter / album_order
--                                         -- album_location / album_category /
--                                            album_notes / album_filter (smart-album DSL,
--                                            preserved verbatim until photo-sorter ships
--                                            its own smart-album evaluator) / album_order
--
-- Every column is added with `ADD COLUMN IF NOT EXISTS` so the migration is
-- idempotent under repeated runs and against partially-applied snapshots.
-- All new columns are NOT NULL with sensible zero-value defaults so existing
-- rows pick up the schema change without a backfill step; migrate-from-
-- photoprism then fills in the values for rows that were imported before
-- this migration landed.

ALTER TABLE subjects ADD COLUMN IF NOT EXISTS bio   TEXT NOT NULL DEFAULT '';
ALTER TABLE subjects ADD COLUMN IF NOT EXISTS about TEXT NOT NULL DEFAULT '';
ALTER TABLE subjects ADD COLUMN IF NOT EXISTS alias TEXT NOT NULL DEFAULT '';

ALTER TABLE labels ADD COLUMN IF NOT EXISTS description TEXT   NOT NULL DEFAULT '';
ALTER TABLE labels ADD COLUMN IF NOT EXISTS categories  TEXT[] NOT NULL DEFAULT '{}';

ALTER TABLE albums ADD COLUMN IF NOT EXISTS location    TEXT NOT NULL DEFAULT '';
ALTER TABLE albums ADD COLUMN IF NOT EXISTS category    TEXT NOT NULL DEFAULT '';
ALTER TABLE albums ADD COLUMN IF NOT EXISTS notes       TEXT NOT NULL DEFAULT '';
ALTER TABLE albums ADD COLUMN IF NOT EXISTS filter      TEXT NOT NULL DEFAULT '';
ALTER TABLE albums ADD COLUMN IF NOT EXISTS album_order TEXT NOT NULL DEFAULT '';
