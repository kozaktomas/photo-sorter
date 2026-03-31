CREATE TABLE text_check_results (
    id SERIAL PRIMARY KEY,
    source_type TEXT NOT NULL,
    source_id TEXT NOT NULL,
    field TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    status TEXT NOT NULL,
    readability_score INTEGER,
    corrected_text TEXT,
    changes JSONB,
    cost_czk NUMERIC(10,4),
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_text_check_source ON text_check_results(source_type, source_id, field);
