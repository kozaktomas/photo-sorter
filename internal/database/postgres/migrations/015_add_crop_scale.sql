ALTER TABLE page_slots ADD COLUMN crop_scale DOUBLE PRECISION NOT NULL DEFAULT 1.0;
ALTER TABLE page_slots ADD CONSTRAINT chk_crop_scale_range CHECK (crop_scale >= 0.1 AND crop_scale <= 1.0);
