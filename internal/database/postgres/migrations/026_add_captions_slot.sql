-- Add is_captions_slot flag for page slots that render the page's photo
-- captions inline (replacing the bottom caption strip). A slot is now in
-- exactly one of four states: photo / text / captions / empty.
ALTER TABLE page_slots
  ADD COLUMN IF NOT EXISTS is_captions_slot BOOLEAN NOT NULL DEFAULT FALSE;

-- Replace migration 012's chk_slot_photo_or_text with a four-state check:
-- at most one of {photo, text, captions} may be set.
ALTER TABLE page_slots DROP CONSTRAINT IF EXISTS chk_slot_photo_or_text;
ALTER TABLE page_slots ADD CONSTRAINT chk_slot_exclusive_content CHECK (
  (CASE WHEN photo_uid IS NOT NULL THEN 1 ELSE 0 END) +
  (CASE WHEN text_content <> ''     THEN 1 ELSE 0 END) +
  (CASE WHEN is_captions_slot       THEN 1 ELSE 0 END) <= 1
);

-- At most one captions slot per page.
CREATE UNIQUE INDEX IF NOT EXISTS uniq_page_captions_slot
  ON page_slots (page_id) WHERE is_captions_slot;
