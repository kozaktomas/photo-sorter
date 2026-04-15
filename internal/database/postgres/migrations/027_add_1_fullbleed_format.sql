-- Add 1_fullbleed page format (single photo covering the entire page including 3mm bleed)
ALTER TABLE book_pages DROP CONSTRAINT IF EXISTS book_pages_format_check;
ALTER TABLE book_pages ADD CONSTRAINT book_pages_format_check
    CHECK (format IN ('4_landscape', '2l_1p', '1p_2l', '2_portrait', '1_fullscreen', '1_fullbleed'));
