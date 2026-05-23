-- Fix the smart_albums and album_share_links FKs to users so deleting
-- a user does not fail with a foreign-key constraint violation.
--
-- Background:
--   * Migration 040 declared `smart_albums.created_by_user_uid VARCHAR(32)
--     NOT NULL REFERENCES users(uid)` without an ON DELETE clause, which
--     defaults to NO ACTION. The migration comment described the intent
--     correctly ("we do NOT cascade on user delete... a smart album
--     survives the deletion of its author and is then effectively
--     orphaned but still usable by other users"), but the schema does not
--     match: a user with any saved search cannot be deleted at all
--     because the FK aborts the DELETE.
--   * Migration 039 has the same bug on `album_share_links.created_by_
--     user_uid`. The link itself is anchored to the album (which uses
--     ON DELETE CASCADE), so removing the user should not revoke the
--     link — the album owns the share, the author column is forensic.
--
-- Fix: drop NOT NULL, drop the implicit FK, recreate as ON DELETE SET
-- NULL so the row outlives its author with `created_by_user_uid` going
-- to NULL. The Go-side scanners are updated in the same commit to
-- COALESCE the column back to an empty string on the wire.
--
-- Idempotent: every statement uses IF EXISTS where supported. Running
-- the migration twice is safe because the schema_migrations bookkeeping
-- prevents that path in practice; the IF EXISTS guards cover the
-- fix-forward case where an operator manually re-runs portions of the
-- migration.

ALTER TABLE smart_albums
    ALTER COLUMN created_by_user_uid DROP NOT NULL;
ALTER TABLE smart_albums
    DROP CONSTRAINT IF EXISTS smart_albums_created_by_user_uid_fkey;
ALTER TABLE smart_albums
    ADD CONSTRAINT smart_albums_created_by_user_uid_fkey
    FOREIGN KEY (created_by_user_uid) REFERENCES users(uid) ON DELETE SET NULL;

ALTER TABLE album_share_links
    ALTER COLUMN created_by_user_uid DROP NOT NULL;
ALTER TABLE album_share_links
    DROP CONSTRAINT IF EXISTS album_share_links_created_by_user_uid_fkey;
ALTER TABLE album_share_links
    ADD CONSTRAINT album_share_links_created_by_user_uid_fkey
    FOREIGN KEY (created_by_user_uid) REFERENCES users(uid) ON DELETE SET NULL;
