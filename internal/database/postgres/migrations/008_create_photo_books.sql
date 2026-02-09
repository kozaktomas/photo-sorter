-- photo_books: top-level entity
CREATE TABLE IF NOT EXISTS photo_books (
    id VARCHAR(36) PRIMARY KEY,
    title VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- book_sections: ordered groups within a book
CREATE TABLE IF NOT EXISTS book_sections (
    id VARCHAR(36) PRIMARY KEY,
    book_id VARCHAR(36) NOT NULL REFERENCES photo_books(id) ON DELETE CASCADE,
    title VARCHAR(255) NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_book_sections_book_id ON book_sections(book_id);

-- section_photos: prepick pool (photo_uid references PhotoPrism)
CREATE TABLE IF NOT EXISTS section_photos (
    id BIGSERIAL PRIMARY KEY,
    section_id VARCHAR(36) NOT NULL REFERENCES book_sections(id) ON DELETE CASCADE,
    photo_uid VARCHAR(32) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(section_id, photo_uid)
);
CREATE INDEX IF NOT EXISTS idx_section_photos_section_id ON section_photos(section_id);

-- book_pages: pages with format and optional section association
CREATE TABLE IF NOT EXISTS book_pages (
    id VARCHAR(36) PRIMARY KEY,
    book_id VARCHAR(36) NOT NULL REFERENCES photo_books(id) ON DELETE CASCADE,
    section_id VARCHAR(36) REFERENCES book_sections(id) ON DELETE SET NULL,
    format VARCHAR(20) NOT NULL CHECK (format IN ('4_landscape', '2l_1p', '2_portrait')),
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_book_pages_book_id ON book_pages(book_id);

-- page_slots: photo assignments to positions on a page
CREATE TABLE IF NOT EXISTS page_slots (
    id BIGSERIAL PRIMARY KEY,
    page_id VARCHAR(36) NOT NULL REFERENCES book_pages(id) ON DELETE CASCADE,
    slot_index INTEGER NOT NULL,
    photo_uid VARCHAR(32),
    UNIQUE(page_id, slot_index),
    UNIQUE(page_id, photo_uid)
);
CREATE INDEX IF NOT EXISTS idx_page_slots_page_id ON page_slots(page_id);
