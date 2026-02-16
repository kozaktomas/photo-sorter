ALTER TABLE page_slots ADD COLUMN crop_x DOUBLE PRECISION NOT NULL DEFAULT 0.5;
ALTER TABLE page_slots ADD COLUMN crop_y DOUBLE PRECISION NOT NULL DEFAULT 0.5;
ALTER TABLE page_slots ADD CONSTRAINT chk_crop_x_range CHECK (crop_x >= 0.0 AND crop_x <= 1.0);
ALTER TABLE page_slots ADD CONSTRAINT chk_crop_y_range CHECK (crop_y >= 0.0 AND crop_y <= 1.0);
ALTER TABLE book_pages ADD COLUMN split_position DOUBLE PRECISION;
ALTER TABLE book_pages ADD CONSTRAINT chk_split_position_range
  CHECK (split_position IS NULL OR (split_position >= 0.2 AND split_position <= 0.8));
