-- Add 1p_2l page format (1 portrait left + 2 landscape right)
ALTER TABLE book_pages DROP CONSTRAINT IF EXISTS book_pages_format_check;
ALTER TABLE book_pages ADD CONSTRAINT book_pages_format_check
    CHECK (format IN ('4_landscape', '2l_1p', '1p_2l', '2_portrait'));
