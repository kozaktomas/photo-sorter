CREATE TABLE IF NOT EXISTS book_chapters (
    id VARCHAR(36) PRIMARY KEY,
    book_id VARCHAR(36) NOT NULL REFERENCES photo_books(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_book_chapters_book_id ON book_chapters(book_id);

ALTER TABLE book_sections ADD COLUMN IF NOT EXISTS chapter_id VARCHAR(36)
    REFERENCES book_chapters(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_book_sections_chapter_id ON book_sections(chapter_id);
