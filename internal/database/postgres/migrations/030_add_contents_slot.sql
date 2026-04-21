-- Add is_contents_slot flag for page slots that render an auto-generated
-- two-column table of contents (book chapters → sections with page ranges).
-- A slot is now in exactly one of five states: photo / text / captions /
-- contents / empty.
ALTER TABLE page_slots
  ADD COLUMN IF NOT EXISTS is_contents_slot BOOLEAN NOT NULL DEFAULT FALSE;

-- Replace migration 026's chk_slot_exclusive_content with a four-way check:
-- at most one of {photo, text, captions, contents} may be set.
ALTER TABLE page_slots DROP CONSTRAINT IF EXISTS chk_slot_exclusive_content;
ALTER TABLE page_slots ADD CONSTRAINT chk_slot_exclusive_content CHECK (
  (CASE WHEN photo_uid IS NOT NULL THEN 1 ELSE 0 END) +
  (CASE WHEN text_content <> ''     THEN 1 ELSE 0 END) +
  (CASE WHEN is_captions_slot       THEN 1 ELSE 0 END) +
  (CASE WHEN is_contents_slot       THEN 1 ELSE 0 END) <= 1
);

-- At most one contents slot per page.
CREATE UNIQUE INDEX IF NOT EXISTS uniq_page_contents_slot
  ON page_slots (page_id) WHERE is_contents_slot;
