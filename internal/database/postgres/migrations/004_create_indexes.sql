-- Additional btree indexes for common queries
CREATE INDEX IF NOT EXISTS idx_faces_photo_uid ON faces(photo_uid);
CREATE INDEX IF NOT EXISTS idx_faces_subject_name ON faces(subject_name) WHERE subject_name IS NOT NULL;
