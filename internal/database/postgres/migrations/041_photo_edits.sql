-- Non-destructive photo edits.
--
-- A row in photo_edits stores the parameters (crop, rotation, brightness,
-- contrast) for the non-destructive edits applied to a single photo. The
-- original file on disk is NEVER modified by this feature — the edits are
-- applied at render time when generating thumbnails, the download stream,
-- and book PDF exports.
--
-- A row exists only when a photo has non-default edits. Absence is treated
-- as "no edits applied", so revert-to-original collapses to a DELETE.
-- The FK to photos(uid) cascades, so hard-deleting a photo automatically
-- drops its edit parameters.
--
-- Crop fields are NULL when the photo has no crop applied. They are stored
-- in 0.0–1.0 relative coordinates against the rotated (display-oriented)
-- image, so cropping survives subsequent rotation changes without needing
-- to re-derive pixel rectangles. Rotation is restricted to multiples of
-- 90° via the CHECK constraint — arbitrary-angle rotation is out of scope.

CREATE TABLE IF NOT EXISTS photo_edits (
    photo_uid   VARCHAR(32)      PRIMARY KEY REFERENCES photos(uid) ON DELETE CASCADE,
    crop_x      REAL             NULL,
    crop_y      REAL             NULL,
    crop_w      REAL             NULL,
    crop_h      REAL             NULL,
    rotation    INTEGER          NOT NULL DEFAULT 0
        CHECK (rotation IN (0, 90, 180, 270)),
    brightness  REAL             NOT NULL DEFAULT 0.0,
    contrast    REAL             NOT NULL DEFAULT 0.0,
    updated_at  TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    CHECK ((crop_x IS NULL AND crop_y IS NULL AND crop_w IS NULL AND crop_h IS NULL) OR
           (crop_x IS NOT NULL AND crop_y IS NOT NULL AND crop_w IS NOT NULL AND crop_h IS NOT NULL))
);
