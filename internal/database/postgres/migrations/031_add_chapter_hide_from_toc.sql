-- Per-chapter flag that hides the chapter (and its sections) from the
-- auto-generated book table of contents rendered by is_contents_slot.
-- The chapter's pages are still rendered normally; only the TOC listing
-- is suppressed. Default FALSE preserves existing TOC behaviour.
ALTER TABLE book_chapters
  ADD COLUMN IF NOT EXISTS hide_from_toc BOOLEAN NOT NULL DEFAULT FALSE;
