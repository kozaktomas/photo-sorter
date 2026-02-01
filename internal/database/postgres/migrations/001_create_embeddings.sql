-- Create pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- Image embeddings table (768-dim CLIP embeddings)
CREATE TABLE IF NOT EXISTS embeddings (
    photo_uid VARCHAR(32) PRIMARY KEY,
    embedding VECTOR(768) NOT NULL,
    model VARCHAR(64) NOT NULL,
    pretrained VARCHAR(64) NOT NULL,
    dim INTEGER NOT NULL DEFAULT 768,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- HNSW index for fast similarity search
CREATE INDEX IF NOT EXISTS idx_embeddings_vector ON embeddings
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 200);
