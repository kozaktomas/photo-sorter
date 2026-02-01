-- Processing status table
CREATE TABLE IF NOT EXISTS faces_processed (
    photo_uid VARCHAR(32) PRIMARY KEY,
    face_count INTEGER NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
