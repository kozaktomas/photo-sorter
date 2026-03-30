CREATE TABLE IF NOT EXISTS text_versions (
    id SERIAL PRIMARY KEY,
    source_type TEXT NOT NULL,
    source_id TEXT NOT NULL,
    field TEXT NOT NULL,
    content TEXT NOT NULL,
    changed_by TEXT NOT NULL DEFAULT 'user',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_text_versions_source ON text_versions(source_type, source_id, field);
