ALTER TABLE book_pages ADD COLUMN IF NOT EXISTS style VARCHAR(20) NOT NULL DEFAULT 'modern';
ALTER TABLE book_pages ADD CONSTRAINT chk_page_style CHECK (style IN ('modern', 'archival'));
