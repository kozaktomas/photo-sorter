-- Perceptual-hash store for native photos. One row per photos.uid; cascades
-- on photo delete so we don't leave dangling hashes behind. pHash/dHash are
-- 64-bit integers stored as BIGINT (PostgreSQL has no native UINT64 — the
-- uint64 value is reinterpreted as int64 via two's-complement bit pattern
-- at the application layer).
--
-- Populated during upload (after the new photos row commits) and via the
-- `photo-sorter cache compute-phashes` backfill command. Used by the
-- upload pipeline's near-duplicate check (pHash hamming distance + CLIP
-- embedding cosine distance).
CREATE TABLE IF NOT EXISTS photo_phashes (
    photo_uid VARCHAR(32) PRIMARY KEY REFERENCES photos(uid) ON DELETE CASCADE,
    phash BIGINT NOT NULL,
    dhash BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
