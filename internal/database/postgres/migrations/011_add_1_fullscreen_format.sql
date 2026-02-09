-- Add 1_fullscreen page format (single fullscreen photo)
ALTER TABLE book_pages DROP CONSTRAINT IF EXISTS book_pages_format_check;
ALTER TABLE book_pages ADD CONSTRAINT book_pages_format_check
    CHECK (format IN ('4_landscape', '2l_1p', '1p_2l', '2_portrait', '1_fullscreen'));
