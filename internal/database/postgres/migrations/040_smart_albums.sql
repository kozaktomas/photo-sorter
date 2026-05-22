-- Smart albums (saved photo searches).
--
-- A smart album is a saved filter expression (label_uids, subject_uids,
-- favorite, taken_from/to, geo bbox, q, sort) that re-evaluates live whenever
-- opened, so it always reflects the current library state. Filters live in a
-- single JSONB column for forward compatibility — the handler validates the
-- shape against the same query-param grammar as `GET /api/v1/photos`.
--
-- The created_by_user_uid column references the users(uid) column added by
-- migration 032. We do NOT cascade on user delete: the spec lists smart
-- albums alongside regular albums (which use ON DELETE SET NULL via the
-- `albums.created_by` column), so a smart album survives the deletion of its
-- author and is then effectively orphaned but still usable by other users.

CREATE TABLE IF NOT EXISTS smart_albums (
    uid                 VARCHAR(32)  PRIMARY KEY,
    name                TEXT         NOT NULL,
    filters             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    created_by_user_uid VARCHAR(32)  NOT NULL REFERENCES users(uid)
);

CREATE INDEX IF NOT EXISTS idx_smart_albums_created_by_user_uid
    ON smart_albums (created_by_user_uid);
