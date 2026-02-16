-- Add text_content column to page_slots for text-only slots
ALTER TABLE page_slots ADD COLUMN IF NOT EXISTS text_content TEXT NOT NULL DEFAULT '';

-- Ensure mutual exclusivity: a slot has either a photo_uid or non-empty text_content, not both
ALTER TABLE page_slots ADD CONSTRAINT chk_slot_photo_or_text
  CHECK (photo_uid IS NULL OR text_content = '');
