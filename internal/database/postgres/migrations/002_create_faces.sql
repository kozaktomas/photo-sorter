-- Face embeddings table (512-dim ResNet100 embeddings)
CREATE TABLE IF NOT EXISTS faces (
    id BIGSERIAL PRIMARY KEY,
    photo_uid VARCHAR(32) NOT NULL,
    face_index INTEGER NOT NULL,
    embedding VECTOR(512) NOT NULL,
    bbox DOUBLE PRECISION[4] NOT NULL,
    det_score DOUBLE PRECISION NOT NULL,
    model VARCHAR(64),
    dim INTEGER NOT NULL DEFAULT 512,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    -- Cached PhotoPrism data
    marker_uid VARCHAR(32),
    subject_uid VARCHAR(32),
    subject_name VARCHAR(255),
    photo_width INTEGER,
    photo_height INTEGER,
    orientation INTEGER,
    file_uid VARCHAR(32),
    UNIQUE(photo_uid, face_index)
);

-- HNSW index for fast similarity search
CREATE INDEX IF NOT EXISTS idx_faces_vector ON faces
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 200);
