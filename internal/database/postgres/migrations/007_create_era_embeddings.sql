-- Era embeddings: CLIP text embedding centroids for photo era estimation
CREATE TABLE IF NOT EXISTS era_embeddings (
    era_slug VARCHAR(64) PRIMARY KEY,
    era_name VARCHAR(255) NOT NULL,
    representative_date DATE NOT NULL,
    prompt_count INTEGER NOT NULL DEFAULT 20,
    embedding VECTOR(768) NOT NULL,
    model VARCHAR(64) NOT NULL,
    pretrained VARCHAR(64) NOT NULL,
    dim INTEGER NOT NULL DEFAULT 768,
    created_at TIMESTAMPTZ DEFAULT NOW()
);
